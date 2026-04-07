#!/usr/bin/env node

"use strict";

const fs = require("fs");
const path = require("path");

function loadPlaywright() {
  const runtimeDir = process.env.QORVEXUS_PLAYWRIGHT_RUNTIME_DIR || "";
  const candidates = [];
  if (runtimeDir) {
    candidates.push(path.join(runtimeDir, "node_modules", "playwright"));
    candidates.push(path.join(runtimeDir, "node_modules", "playwright-core"));
  }
  candidates.push("playwright");
  candidates.push("playwright-core");

  for (const candidate of candidates) {
    try {
      return require(candidate);
    } catch (_err) {
    }
  }

  const message = [
    "Playwright package is not installed for Qorvexus.",
    "Start the service once to let Qorvexus auto-install it, or run `npm install playwright` manually.",
  ].join(" ");
  console.error(message);
  process.exit(1);
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

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function ensureDir(dir) {
  if (dir) {
    fs.mkdirSync(dir, { recursive: true });
  }
}

function sanitizeName(value, fallback) {
  const raw = String(value || "").trim().toLowerCase();
  if (!raw) {
    return fallback;
  }
  const cleaned = raw.replace(/[^a-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
  return cleaned || fallback;
}

function resolveArtifactPath(artifactsDir, name, fallbackExt) {
  const cleaned = String(name || "").trim();
  if (!cleaned) {
    return path.join(artifactsDir, `${Date.now()}${fallbackExt}`);
  }
  if (path.isAbsolute(cleaned)) {
    ensureDir(path.dirname(cleaned));
    return cleaned;
  }
  const target = path.join(artifactsDir, cleaned);
  ensureDir(path.dirname(target));
  return target;
}

async function withRetry(label, attempts, fn) {
  let lastError = null;
  const total = attempts > 0 ? attempts : 1;
  for (let index = 1; index <= total; index += 1) {
    try {
      return await fn(index);
    } catch (err) {
      lastError = err;
      if (index < total) {
        await sleep(300 * index);
      }
    }
  }
  throw new Error(`${label} failed after ${total} attempt(s): ${lastError ? lastError.message : "unknown error"}`);
}

async function waitForAction(page, action, defaultTimeoutMs) {
  const timeout = action.timeout_seconds ? action.timeout_seconds * 1000 : defaultTimeoutMs;
  if (action.selector) {
    await page.locator(action.selector).first().waitFor({
      state: action.state || "visible",
      timeout,
    });
    return { selector: action.selector, state: action.state || "visible" };
  }
  if (action.text) {
    await page.getByText(action.text, { exact: Boolean(action.exact) }).first().waitFor({
      state: action.state || "visible",
      timeout,
    });
    return { text: action.text, state: action.state || "visible" };
  }
  throw new Error("wait_for action requires selector or text");
}

async function extractTable(page, selector) {
  return page.locator(selector).first().evaluate((table) => {
    const rows = Array.from(table.querySelectorAll("tr"));
    const headers = rows.length > 0
      ? Array.from(rows[0].querySelectorAll("th,td")).map((cell) => cell.textContent.trim())
      : [];
    const bodyRows = rows.slice(headers.length > 0 ? 1 : 0).map((row) =>
      Array.from(row.querySelectorAll("th,td")).map((cell) => cell.textContent.trim())
    );
    return { headers, rows: bodyRows };
  });
}

function listDownloadedFiles(artifactsDir) {
  const downloadsDir = path.join(artifactsDir, "downloads");
  if (!fs.existsSync(downloadsDir)) {
    return [];
  }
  return fs.readdirSync(downloadsDir).map((name) => {
    const fullPath = path.join(downloadsDir, name);
    const stat = fs.statSync(fullPath);
    return {
      name,
      path: fullPath,
      size_bytes: stat.size,
      modified_at: stat.mtime.toISOString(),
    };
  });
}

async function runAction(page, context, action, state) {
  const type = String(action.type || "").trim().toLowerCase();
  const retryCount = Number.isFinite(action.retry_count) ? action.retry_count : state.retryCount;
  const timeoutMs = action.timeout_seconds ? action.timeout_seconds * 1000 : state.timeoutMs;

  return withRetry(type || "browser_action", retryCount, async () => {
    switch (type) {
      case "goto": {
        const response = await page.goto(action.url, {
          waitUntil: action.wait_until || "networkidle",
          timeout: timeoutMs,
        });
        return {
          type,
          url: page.url(),
          status: response ? response.status() : null,
        };
      }
      case "wait_for": {
        const waited = await waitForAction(page, action, timeoutMs);
        return { type, waited };
      }
      case "click": {
        const locator = page.locator(action.selector).first();
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        if (action.wait_for_navigation) {
          await Promise.all([
            page.waitForLoadState(action.load_state || "networkidle", { timeout: timeoutMs }),
            locator.click({
              button: action.button || "left",
              clickCount: action.click_count || 1,
              timeout: timeoutMs,
            }),
          ]);
        } else {
          await locator.click({
            button: action.button || "left",
            clickCount: action.click_count || 1,
            timeout: timeoutMs,
          });
        }
        return { type, selector: action.selector, url: page.url() };
      }
      case "type": {
        const locator = page.locator(action.selector).first();
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        if (action.clear !== false) {
          await locator.fill(String(action.text || ""), { timeout: timeoutMs });
        } else {
          await locator.type(String(action.text || ""), { timeout: timeoutMs });
        }
        return { type, selector: action.selector, text_length: String(action.text || "").length };
      }
      case "press": {
        const locator = page.locator(action.selector).first();
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        await locator.press(String(action.key || "Enter"), { timeout: timeoutMs });
        return { type, selector: action.selector, key: action.key || "Enter" };
      }
      case "login_form": {
        if (!action.username_selector || !action.password_selector) {
          throw new Error("login_form requires username_selector and password_selector");
        }
        await page.locator(action.username_selector).first().fill(String(action.username || ""), { timeout: timeoutMs });
        await page.locator(action.password_selector).first().fill(String(action.password || ""), { timeout: timeoutMs });
        if (action.submit_selector) {
          await page.locator(action.submit_selector).first().click({ timeout: timeoutMs });
        } else {
          await page.locator(action.password_selector).first().press("Enter", { timeout: timeoutMs });
        }
        if (action.wait_for_selector) {
          await page.locator(action.wait_for_selector).first().waitFor({ state: "visible", timeout: timeoutMs });
        } else if (action.wait_for_navigation !== false) {
          await page.waitForLoadState(action.load_state || "networkidle", { timeout: timeoutMs }).catch(() => {});
        }
        return { type, username: String(action.username || ""), url: page.url() };
      }
      case "extract_table": {
        const table = await extractTable(page, action.selector);
        return { type, selector: action.selector, name: action.name || "", table };
      }
      case "extract_text": {
        const locator = page.locator(action.selector).first();
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        const text = (await locator.textContent()) || "";
        return { type, selector: action.selector, text: text.trim() };
      }
      case "expect_text": {
        if (!action.text) {
          throw new Error("expect_text requires text");
        }
        if (action.selector) {
          const locator = page.locator(action.selector).first();
          await locator.waitFor({ state: "visible", timeout: timeoutMs });
          const content = (await locator.textContent()) || "";
          if (!content.includes(action.text)) {
            throw new Error(`expected text ${action.text} inside ${action.selector}`);
          }
        } else {
          await page.getByText(action.text, { exact: Boolean(action.exact) }).first().waitFor({
            state: "visible",
            timeout: timeoutMs,
          });
        }
        return { type, text: action.text, selector: action.selector || "" };
      }
      case "screenshot": {
        const target = resolveArtifactPath(state.artifactsDir, action.path || `screenshot-${Date.now()}.png`, ".png");
        await page.screenshot({
          path: target,
          fullPage: Boolean(action.full_page),
          timeout: timeoutMs,
        });
        return { type, path: target };
      }
      case "save_pdf": {
        if (state.browserName !== "chromium") {
          throw new Error("save_pdf currently requires chromium");
        }
        const target = resolveArtifactPath(state.artifactsDir, action.path || `page-${Date.now()}.pdf`, ".pdf");
        await page.pdf({
          path: target,
          format: action.format || "A4",
          landscape: Boolean(action.landscape),
          printBackground: action.print_background !== false,
        });
        return { type, path: target };
      }
      case "download": {
        if (!action.selector) {
          throw new Error("download action requires selector");
        }
        const downloadsDir = path.join(state.artifactsDir, "downloads");
        ensureDir(downloadsDir);
        const downloadPromise = page.waitForEvent("download", { timeout: timeoutMs });
        await page.locator(action.selector).first().click({ timeout: timeoutMs });
        const download = await downloadPromise;
        const fileName = sanitizeName(download.suggestedFilename(), `download-${Date.now()}`);
        const target = resolveArtifactPath(downloadsDir, fileName, "");
        await download.saveAs(target);
        const item = {
          suggested_filename: download.suggestedFilename(),
          path: target,
          url: typeof download.url === "function" ? download.url() : "",
        };
        state.downloads.push(item);
        return { type, download: item };
      }
      case "list_downloads": {
        const files = listDownloadedFiles(state.artifactsDir);
        return { type, downloads: files };
      }
      default:
        throw new Error(`unsupported browser action: ${type}`);
    }
  });
}

async function runActionsMode(page, context, qorvexus) {
  const inputPath = process.env.QORVEXUS_PLAYWRIGHT_ACTIONS_FILE;
  if (!inputPath) {
    throw new Error("QORVEXUS_PLAYWRIGHT_ACTIONS_FILE is required in actions mode");
  }
  const input = JSON.parse(fs.readFileSync(inputPath, "utf8"));
  const actions = Array.isArray(input.actions) ? input.actions : [];
  if (actions.length === 0) {
    throw new Error("browser workflow requires at least one action");
  }
  const state = {
    browserName: qorvexus.browserName,
    artifactsDir: qorvexus.artifactsDir,
    downloads: [],
    retryCount: Number.isFinite(input.retry_count) && input.retry_count > 0 ? input.retry_count : 1,
    timeoutMs: qorvexus.timeoutSeconds * 1000,
  };
  const results = [];
  if (input.start_url) {
    results.push(await runAction(page, context, { type: "goto", url: input.start_url, wait_until: "networkidle" }, state));
  }
  for (const action of actions) {
    results.push(await runAction(page, context, action, state));
  }
  return {
    mode: "actions",
    current_url: page.url(),
    downloads: listDownloadedFiles(qorvexus.artifactsDir),
    results,
  };
}

async function runScriptMode(page, context, playwright, browserType, qorvexus) {
  const scriptPath = process.env.QORVEXUS_PLAYWRIGHT_SCRIPT_FILE;
  if (!scriptPath) {
    throw new Error("QORVEXUS_PLAYWRIGHT_SCRIPT_FILE is required in script mode");
  }
  const script = fs.readFileSync(scriptPath, "utf8");
  const wrapped = new Function(
    "playwright",
    "browserType",
    "context",
    "page",
    "qorvexus",
    `return (async () => {\n${script}\n})();`
  );
  return wrapped(playwright, browserType, context, page, qorvexus);
}

async function main() {
  const mode = (process.env.QORVEXUS_PLAYWRIGHT_MODE || "script").trim().toLowerCase();
  const browserName = process.env.QORVEXUS_PLAYWRIGHT_BROWSER || "chromium";
  const profileDir = process.env.QORVEXUS_PLAYWRIGHT_PROFILE_DIR || "";
  const storageStatePath = process.env.QORVEXUS_PLAYWRIGHT_STORAGE_STATE_FILE || "";
  const artifactsDir = process.env.QORVEXUS_PLAYWRIGHT_ARTIFACTS_DIR || "";
  const persistProfile = envBool("QORVEXUS_PLAYWRIGHT_PERSIST_PROFILE", true);
  const saveStorageState = envBool("QORVEXUS_PLAYWRIGHT_SAVE_STORAGE_STATE", true);
  const headless = envBool("QORVEXUS_PLAYWRIGHT_HEADLESS", true);
  const timeoutSeconds = envInt("QORVEXUS_PLAYWRIGHT_TIMEOUT_SECONDS", 120);

  ensureDir(artifactsDir);
  if (profileDir && persistProfile) {
    ensureDir(profileDir);
  }
  if (storageStatePath) {
    ensureDir(path.dirname(storageStatePath));
  }

  const playwright = loadPlaywright();
  const browserType = playwright[browserName];
  if (!browserType) {
    throw new Error(`unsupported browser type: ${browserName}`);
  }

  let browser = null;
  let context = null;
  let page = null;
  const qorvexus = {
    browserName,
    profileDir,
    storageStatePath,
    artifactsDir,
    timeoutSeconds,
    log(value) {
      process.stdout.write(`${String(value)}\n`);
    },
    artifactPath(name, fallbackExt) {
      return resolveArtifactPath(artifactsDir, name, fallbackExt || "");
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
    withRetry,
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

    let result;
    if (mode === "actions") {
      result = await runActionsMode(page, context, qorvexus);
    } else {
      result = await runScriptMode(page, context, playwright, browserType, qorvexus);
    }

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
