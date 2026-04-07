#!/usr/bin/env node

"use strict";

const fs = require("fs");
const path = require("path");

function loadPlaywright() {
  try {
    return require("playwright");
  } catch (firstErr) {
    try {
      return require("playwright-core");
    } catch (secondErr) {
      const message = [
        "Playwright package is not installed for Qorvexus.",
        "Install it with `npm install playwright` or point tools.playwright_command at a custom runner.",
      ].join(" ");
      console.error(message);
      process.exit(1);
    }
  }
}

function envBool(name, defaultValue) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") {
    return defaultValue;
  }
  return raw.toLowerCase() === "true";
}

function envInt(name, defaultValue) {
  const raw = process.env[name];
  if (!raw) {
    return defaultValue;
  }
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) ? parsed : defaultValue;
}

function stringifyResult(value) {
  if (value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value, null, 2);
}

async function main() {
  const scriptPath = process.env.QORVEXUS_PLAYWRIGHT_SCRIPT_FILE;
  if (!scriptPath) {
    throw new Error("QORVEXUS_PLAYWRIGHT_SCRIPT_FILE is required");
  }
  const script = fs.readFileSync(scriptPath, "utf8");
  const browserName = process.env.QORVEXUS_PLAYWRIGHT_BROWSER || "chromium";
  const profileDir = process.env.QORVEXUS_PLAYWRIGHT_PROFILE_DIR || "";
  const storageStatePath = process.env.QORVEXUS_PLAYWRIGHT_STORAGE_STATE_FILE || "";
  const artifactsDir = process.env.QORVEXUS_PLAYWRIGHT_ARTIFACTS_DIR || "";
  const persistProfile = envBool("QORVEXUS_PLAYWRIGHT_PERSIST_PROFILE", true);
  const saveStorageState = envBool("QORVEXUS_PLAYWRIGHT_SAVE_STORAGE_STATE", true);
  const headless = envBool("QORVEXUS_PLAYWRIGHT_HEADLESS", true);
  const timeoutSeconds = envInt("QORVEXUS_PLAYWRIGHT_TIMEOUT_SECONDS", 120);

  if (artifactsDir) {
    fs.mkdirSync(artifactsDir, { recursive: true });
  }
  if (profileDir && persistProfile) {
    fs.mkdirSync(profileDir, { recursive: true });
  }
  if (storageStatePath) {
    fs.mkdirSync(path.dirname(storageStatePath), { recursive: true });
  }

  const playwright = loadPlaywright();
  const browserType = playwright[browserName];
  if (!browserType) {
    throw new Error(`unsupported browser type: ${browserName}`);
  }

  let browser = null;
  let context = null;
  let page = null;
  const helpers = {
    browserName,
    profileDir,
    storageStatePath,
    artifactsDir,
    timeoutSeconds,
    log(value) {
      process.stdout.write(`${String(value)}\n`);
    },
    async ensurePage() {
      if (page) {
        return page;
      }
      if (context.pages().length > 0) {
        page = context.pages()[0];
        return page;
      }
      page = await context.newPage();
      return page;
    },
    async saveStorageState() {
      if (storageStatePath) {
        await context.storageState({ path: storageStatePath });
      }
    },
  };

  const timeoutHandle = setTimeout(() => {
    console.error(`Playwright run exceeded ${timeoutSeconds} seconds`);
    process.exit(124);
  }, timeoutSeconds * 1000);

  try {
    if (persistProfile) {
      context = await browserType.launchPersistentContext(profileDir, {
        headless,
        acceptDownloads: true,
        downloadsPath: artifactsDir || undefined,
      });
    } else {
      browser = await browserType.launch({
        headless,
        downloadsPath: artifactsDir || undefined,
      });
      const contextOptions = { acceptDownloads: true };
      if (storageStatePath && fs.existsSync(storageStatePath)) {
        contextOptions.storageState = storageStatePath;
      }
      context = await browser.newContext(contextOptions);
    }

    context.setDefaultTimeout(timeoutSeconds * 1000);
    if (context.pages().length > 0) {
      page = context.pages()[0];
    } else {
      page = await context.newPage();
    }

    const wrapped = new Function(
      "playwright",
      "browserType",
      "context",
      "page",
      "qorvexus",
      `return (async () => {\n${script}\n})();`
    );
    const result = await wrapped(playwright, browserType, context, page, helpers);

    if (saveStorageState && storageStatePath) {
      await context.storageState({ path: storageStatePath });
    }

    const text = stringifyResult(result);
    if (text) {
      process.stdout.write(text);
      if (!text.endsWith("\n")) {
        process.stdout.write("\n");
      }
    }
  } finally {
    clearTimeout(timeoutHandle);
    if (context) {
      await context.close();
    }
    if (browser) {
      await browser.close();
    }
  }
}

main().catch((err) => {
  console.error(err && err.stack ? err.stack : String(err));
  process.exit(1);
});
