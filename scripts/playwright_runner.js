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

async function withOverallTimeout(promise, timeoutMs, label) {
  let timeoutHandle = null;
  const timeout = new Promise((_, reject) => {
    timeoutHandle = setTimeout(() => reject(new Error(`${label} exceeded ${Math.round(timeoutMs / 1000)} seconds`)), timeoutMs);
  });
  try {
    return await Promise.race([promise, timeout]);
  } finally {
    clearTimeout(timeoutHandle);
  }
}

function escapeCssAttribute(value) {
  return String(value || "").replace(/\\/g, "\\\\").replace(/"/g, "\\\"");
}

function normalizeBoolean(value) {
  if (typeof value === "boolean") {
    return value;
  }
  if (typeof value === "number") {
    return value !== 0;
  }
  if (typeof value === "string") {
    const lowered = value.trim().toLowerCase();
    return lowered === "true" || lowered === "yes" || lowered === "1" || lowered === "on";
  }
  return Boolean(value);
}

function isNonEmptyString(value) {
  return typeof value === "string" && value.trim() !== "";
}

function compactText(value, limit) {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  if (!limit || text.length <= limit) {
    return text;
  }
  return `${text.slice(0, limit)}...`;
}

async function maybeWaitForLoadState(page, loadState, timeoutMs) {
  await page.waitForLoadState(loadState || "domcontentloaded", { timeout: timeoutMs }).catch(() => {});
}

async function waitForAction(page, action, defaultTimeoutMs) {
  const timeout = action.timeout_seconds ? action.timeout_seconds * 1000 : defaultTimeoutMs;
  if (action.url_contains) {
    await page.waitForURL(`**${action.url_contains}**`, { timeout });
    return { url_contains: action.url_contains };
  }
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
  throw new Error("wait_for action requires selector, text, or url_contains");
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

function refreshTabState(context, state) {
  state.pages = context.pages();
  if (!state.currentPage && state.pages.length > 0) {
    state.currentPage = state.pages[0];
  }
  if (state.currentPage && !state.pages.includes(state.currentPage)) {
    state.currentPage = state.pages.length > 0 ? state.pages[Math.max(0, state.pages.length - 1)] : null;
  }
  state.currentTabIndex = state.currentPage ? state.pages.indexOf(state.currentPage) : -1;
  return state;
}

async function ensureCurrentPage(context, state) {
  refreshTabState(context, state);
  if (state.currentPage) {
    return state.currentPage;
  }
  state.currentPage = await context.newPage();
  refreshTabState(context, state);
  return state.currentPage;
}

async function collectTabsInfo(context, state) {
  refreshTabState(context, state);
  const tabs = [];
  for (let index = 0; index < state.pages.length; index += 1) {
    const candidate = state.pages[index];
    let title = "";
    try {
      title = await candidate.title();
    } catch (_err) {
    }
    tabs.push({
      index,
      current: index === state.currentTabIndex,
      url: typeof candidate.url === "function" ? candidate.url() : "",
      title,
    });
  }
  return tabs;
}

async function resolveTargetTabIndex(context, state, action) {
  refreshTabState(context, state);
  if (state.pages.length === 0) {
    return -1;
  }
  if (Number.isInteger(action.index)) {
    return action.index >= 0 && action.index < state.pages.length ? action.index : -1;
  }
  if (action.current === true) {
    return state.currentTabIndex >= 0 ? state.currentTabIndex : 0;
  }
  if (action.newest === true) {
    return state.pages.length - 1;
  }
  if (isNonEmptyString(action.url_contains)) {
    const lowered = action.url_contains.toLowerCase();
    for (let index = 0; index < state.pages.length; index += 1) {
      const currentUrl = state.pages[index].url();
      if (String(currentUrl || "").toLowerCase().includes(lowered)) {
        return index;
      }
    }
  }
  if (isNonEmptyString(action.title_contains)) {
    const lowered = action.title_contains.toLowerCase();
    for (let index = 0; index < state.pages.length; index += 1) {
      try {
        const title = await state.pages[index].title();
        if (String(title || "").toLowerCase().includes(lowered)) {
          return index;
        }
      } catch (_err) {
      }
    }
  }
  return state.currentTabIndex >= 0 ? state.currentTabIndex : 0;
}

async function locatorExists(locator) {
  try {
    return (await locator.count()) > 0;
  } catch (_err) {
    return false;
  }
}

async function locatorVisible(locator) {
  try {
    return await locator.first().isVisible();
  } catch (_err) {
    return false;
  }
}

async function firstVisibleLocator(candidates) {
  for (const candidate of candidates) {
    if (candidate && await locatorVisible(candidate)) {
      return candidate.first();
    }
  }
  for (const candidate of candidates) {
    if (candidate && await locatorExists(candidate)) {
      return candidate.first();
    }
  }
  return null;
}

async function locateBySelectorOrText(page, action) {
  const candidates = [];
  if (isNonEmptyString(action.selector)) {
    candidates.push(page.locator(action.selector));
  }
  if (isNonEmptyString(action.text)) {
    candidates.push(page.getByText(action.text, { exact: Boolean(action.exact) }));
  }
  if (isNonEmptyString(action.label)) {
    candidates.push(page.getByLabel(action.label, { exact: Boolean(action.exact) }));
  }
  if (isNonEmptyString(action.placeholder)) {
    candidates.push(page.getByPlaceholder(action.placeholder, { exact: Boolean(action.exact) }));
  }
  if (isNonEmptyString(action.role) && isNonEmptyString(action.name)) {
    candidates.push(page.getByRole(action.role, { name: action.name, exact: Boolean(action.exact) }));
  }
  return firstVisibleLocator(candidates);
}

async function isEvidenceVisible(page, descriptor, timeoutMs) {
  if (!descriptor || (!descriptor.selector && !descriptor.text)) {
    return false;
  }
  try {
    if (descriptor.selector) {
      await page.locator(descriptor.selector).first().waitFor({ state: "visible", timeout: timeoutMs });
      return true;
    }
    await page.getByText(descriptor.text, { exact: Boolean(descriptor.exact) }).first().waitFor({
      state: "visible",
      timeout: timeoutMs,
    });
    return true;
  } catch (_err) {
    return false;
  }
}

async function detectLoginState(page, action, timeoutMs) {
  const loggedIn = await isEvidenceVisible(page, {
    selector: action.logged_in_selector,
    text: action.logged_in_text,
    exact: action.logged_in_exact,
  }, timeoutMs);
  if (loggedIn) {
    return "logged_in";
  }

  const loggedOut = await isEvidenceVisible(page, {
    selector: action.logged_out_selector,
    text: action.logged_out_text,
    exact: action.logged_out_exact,
  }, timeoutMs);
  if (loggedOut) {
    return "logged_out";
  }

  const currentUrl = String(page.url() || "").toLowerCase();
  if (/(login|sign-in|signin|auth)/.test(currentUrl)) {
    return "logged_out";
  }
  return "unknown";
}

function normalizeUploadFiles(action) {
  const rawItems = [];
  if (Array.isArray(action.files)) {
    rawItems.push(...action.files);
  }
  if (action.file !== undefined && action.file !== null && action.file !== "") {
    rawItems.push(action.file);
  }
  if (rawItems.length === 0) {
    throw new Error("upload_files requires file or files");
  }

  return rawItems.map((item) => {
    if (typeof item === "string") {
      return path.resolve(item);
    }
    if (!item || typeof item !== "object") {
      throw new Error("upload_files entries must be strings or objects");
    }
    const name = String(item.name || `upload-${Date.now()}.txt`);
    const payload = { name };
    if (isNonEmptyString(item.mime_type)) {
      payload.mimeType = item.mime_type;
    }
    if (isNonEmptyString(item.text)) {
      payload.buffer = Buffer.from(item.text, "utf8");
      return payload;
    }
    if (isNonEmptyString(item.content_base64)) {
      payload.buffer = Buffer.from(item.content_base64, "base64");
      return payload;
    }
    if (isNonEmptyString(item.path)) {
      return path.resolve(item.path);
    }
    throw new Error("upload_files object entries require path, text, or content_base64");
  });
}

function normalizeFormEntries(fields) {
  if (Array.isArray(fields)) {
    return fields.map((item) => ({
      name: String(item.name || item.field || "").trim(),
      value: item.value,
    })).filter((item) => item.name);
  }
  if (fields && typeof fields === "object") {
    return Object.entries(fields).map(([name, value]) => ({ name, value }));
  }
  return [];
}

async function resolveFormField(page, fieldName) {
  const exact = false;
  const escaped = escapeCssAttribute(fieldName);
  const broadSelector = [
    `input[name*="${escaped}" i]`,
    `textarea[name*="${escaped}" i]`,
    `select[name*="${escaped}" i]`,
    `input[id*="${escaped}" i]`,
    `textarea[id*="${escaped}" i]`,
    `select[id*="${escaped}" i]`,
    `input[aria-label*="${escaped}" i]`,
    `textarea[aria-label*="${escaped}" i]`,
    `select[aria-label*="${escaped}" i]`,
    `input[placeholder*="${escaped}" i]`,
    `textarea[placeholder*="${escaped}" i]`,
  ].join(", ");
  const candidates = [
    page.getByLabel(fieldName, { exact }).first(),
    page.getByPlaceholder(fieldName, { exact }).first(),
    page.locator(`[name="${escaped}"]`).first(),
    page.locator(`[id="${escaped}"]`).first(),
    page.locator(`[aria-label="${escaped}"]`).first(),
    page.locator(broadSelector).first(),
  ];

  const visible = await firstVisibleLocator(candidates);
  if (visible) {
    return visible;
  }

  const labels = page.locator("label").filter({ hasText: fieldName }).first();
  if (await locatorVisible(labels) || await locatorExists(labels)) {
    const labelledControl = labels.locator("input,textarea,select").first();
    if (await locatorVisible(labelledControl) || await locatorExists(labelledControl)) {
      return labelledControl;
    }
    const labelFor = await labels.getAttribute("for");
    if (labelFor) {
      const linked = page.locator(`#${escapeCssAttribute(labelFor)}`).first();
      if (await locatorExists(linked)) {
        return linked;
      }
    }
  }

  throw new Error(`unable to locate form field ${fieldName}`);
}

async function applyFieldValue(page, locator, fieldName, value, timeoutMs) {
  const metadata = await locator.evaluate((element) => ({
    tag: element.tagName.toLowerCase(),
    type: String(element.getAttribute("type") || "").toLowerCase(),
    name: String(element.getAttribute("name") || ""),
  }));

  if (metadata.tag === "select") {
    if (Array.isArray(value)) {
      await locator.selectOption(value.map((item) => String(item)));
    } else if (value && typeof value === "object") {
      const option = {};
      if (value.value !== undefined) {
        option.value = String(value.value);
      }
      if (value.label !== undefined) {
        option.label = String(value.label);
      }
      await locator.selectOption(option);
    } else {
      const scalar = String(value);
      try {
        await locator.selectOption({ label: scalar });
      } catch (_err) {
        await locator.selectOption(scalar);
      }
    }
    return { field: fieldName, mode: "select", value: Array.isArray(value) ? value : String(value) };
  }

  if (metadata.type === "checkbox") {
    const checked = normalizeBoolean(value);
    if (checked) {
      await locator.check({ timeout: timeoutMs });
    } else {
      await locator.uncheck({ timeout: timeoutMs });
    }
    return { field: fieldName, mode: "checkbox", value: checked };
  }

  if (metadata.type === "radio") {
    if (metadata.name && isNonEmptyString(value)) {
      const radioLocator = page.locator(
        `input[type="radio"][name="${escapeCssAttribute(metadata.name)}"][value="${escapeCssAttribute(String(value))}"]`
      ).first();
      if (await locatorExists(radioLocator)) {
        await radioLocator.check({ timeout: timeoutMs });
        return { field: fieldName, mode: "radio", value: String(value) };
      }
    }
    await locator.check({ timeout: timeoutMs });
    return { field: fieldName, mode: "radio", value: true };
  }

  if (metadata.type === "file") {
    const files = normalizeUploadFiles({ files: Array.isArray(value) ? value : [value] });
    await locator.setInputFiles(files);
    return { field: fieldName, mode: "file", count: files.length };
  }

  const textValue = value === undefined || value === null ? "" : String(value);
  await locator.fill(textValue, { timeout: timeoutMs });
  return { field: fieldName, mode: metadata.type || metadata.tag || "text", value: textValue };
}

async function triggerSubmit(page, action, timeoutMs) {
  if (isNonEmptyString(action.submit_selector)) {
    const locator = page.locator(action.submit_selector).first();
    await locator.waitFor({ state: "visible", timeout: timeoutMs });
    if (action.wait_for_navigation) {
      await Promise.all([
        maybeWaitForLoadState(page, action.load_state || "domcontentloaded", timeoutMs),
        locator.click({ timeout: timeoutMs }),
      ]);
    } else {
      await locator.click({ timeout: timeoutMs });
    }
    return { mode: "selector", selector: action.submit_selector };
  }

  if (isNonEmptyString(action.submit_text)) {
    const locator = page.getByRole("button", { name: action.submit_text }).first();
    if (await locatorExists(locator)) {
      await locator.click({ timeout: timeoutMs });
      return { mode: "text", text: action.submit_text };
    }
    const fallback = page.getByText(action.submit_text, { exact: Boolean(action.submit_exact) }).first();
    await fallback.click({ timeout: timeoutMs });
    return { mode: "text", text: action.submit_text };
  }

  if (action.submit === true) {
    await page.keyboard.press(String(action.submit_key || "Enter"));
    return { mode: "key", key: String(action.submit_key || "Enter") };
  }

  return null;
}

function normalizeFieldSpecs(fields) {
  if (!fields || typeof fields !== "object" || Array.isArray(fields)) {
    return [];
  }
  return Object.entries(fields).map(([name, spec]) => {
    if (typeof spec === "string") {
      const at = spec.lastIndexOf("@");
      if (at > 0) {
        return { name, selector: spec.slice(0, at), attr: spec.slice(at + 1) };
      }
      return { name, selector: spec };
    }
    if (spec && typeof spec === "object") {
      return {
        name,
        selector: spec.selector || "",
        attr: spec.attr || "",
        html: Boolean(spec.html),
      };
    }
    return { name, selector: "", attr: "", html: false };
  });
}

async function extractPaginatedItems(page, action) {
  const specs = normalizeFieldSpecs(action.fields);
  return page.locator(action.item_selector).evaluateAll((nodes, cfg) => nodes.map((node, index) => {
    if (!cfg.specs.length) {
      return {
        index,
        text: String(node.textContent || "").trim(),
      };
    }
    const item = { index };
    for (const spec of cfg.specs) {
      let target = node;
      if (spec.selector) {
        target = node.querySelector(spec.selector);
      }
      if (!target) {
        item[spec.name] = null;
        continue;
      }
      if (spec.attr) {
        item[spec.name] = target.getAttribute(spec.attr);
      } else if (spec.html) {
        item[spec.name] = target.innerHTML.trim();
      } else {
        item[spec.name] = String(target.textContent || "").trim();
      }
    }
    return item;
  }), { specs });
}

async function resolvePaginationNextLocator(page, action) {
  if (isNonEmptyString(action.next_selector)) {
    const locator = page.locator(action.next_selector).first();
    if (await locatorExists(locator)) {
      return locator;
    }
  }
  if (isNonEmptyString(action.next_text)) {
    const locator = page.getByText(action.next_text, { exact: Boolean(action.next_exact) }).first();
    if (await locatorExists(locator)) {
      return locator;
    }
  }
  return null;
}

async function isLocatorDisabled(locator) {
  try {
    return await locator.evaluate((element) => (
      element.hasAttribute("disabled") ||
      element.getAttribute("aria-disabled") === "true" ||
      element.closest("[aria-disabled='true']") !== null
    ));
  } catch (_err) {
    return false;
  }
}

async function observePage(page, options = {}) {
  const maxItems = Number.isFinite(options.max_items) && options.max_items > 0 ? options.max_items : 40;
  const textLimit = Number.isFinite(options.text_limit) && options.text_limit > 0 ? options.text_limit : 1200;
  const title = await page.title().catch(() => "");
  const url = typeof page.url === "function" ? page.url() : "";
  const visibleText = await page.locator("body").innerText({ timeout: 1000 }).catch(() => "");
  const interactive = await page.evaluate((limit) => {
    function visible(el) {
      const style = window.getComputedStyle(el);
      const rect = el.getBoundingClientRect();
      return style && style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0;
    }
    function textOf(el) {
      return String(el.innerText || el.textContent || el.getAttribute("aria-label") || el.getAttribute("title") || el.getAttribute("placeholder") || el.getAttribute("name") || "").replace(/\s+/g, " ").trim().slice(0, 120);
    }
    function cssIdent(value) {
      if (window.CSS && typeof window.CSS.escape === "function") {
        return window.CSS.escape(value);
      }
      return String(value).replace(/["\\]/g, "\\$&");
    }
    function selectorOf(el) {
      if (el.id) {
        return `#${cssIdent(el.id)}`;
      }
      const name = el.getAttribute("name");
      if (name) {
        return `${el.tagName.toLowerCase()}[name="${String(name).replace(/"/g, '\\"')}"]`;
      }
      const aria = el.getAttribute("aria-label");
      if (aria) {
        return `${el.tagName.toLowerCase()}[aria-label="${String(aria).replace(/"/g, '\\"')}"]`;
      }
      const href = el.getAttribute("href");
      if (href && el.tagName.toLowerCase() === "a") {
        return `a[href="${String(href).replace(/"/g, '\\"')}"]`;
      }
      return el.tagName.toLowerCase();
    }
    const nodes = Array.from(document.querySelectorAll("a[href],button,input,textarea,select,[role='button'],[role='link']"))
      .filter(visible)
      .slice(0, limit);
    return nodes.map((el, index) => {
      const tag = el.tagName.toLowerCase();
      const role = el.getAttribute("role") || (tag === "a" ? "link" : tag === "button" ? "button" : tag);
      const item = {
        index,
        role,
        text: textOf(el),
        selector: selectorOf(el),
      };
      if (tag === "a" && el.href) {
        item.href = el.href;
      }
      const type = el.getAttribute("type");
      if (type) {
        item.type = type;
      }
      return item;
    });
  }, maxItems).catch(() => []);
  return {
    title,
    url,
    text: compactText(visibleText, textLimit),
    interactive,
    hint: "Use selector/text/label/placeholder from interactive items for the next browser_workflow action.",
  };
}

function normalizeActionDefaults(rawAction) {
  const action = { ...(rawAction || {}) };
  let type = String(action.type || "").trim().toLowerCase();
  const aliases = {
    navigate: "goto",
    open: "goto",
    wait: "wait_for",
    fill: "type",
    input: "type",
    text: "extract_text",
    snapshot: "observe",
    inspect: "observe",
    login: "login_form",
  };
  type = aliases[type] || type;
  if (!type) {
    if (isNonEmptyString(action.url) || isNonEmptyString(action.href)) {
      type = "goto";
    } else if ((action.selector || action.text) && (action.value !== undefined || action.input !== undefined)) {
      type = "type";
    } else if (action.selector || action.text) {
      type = "click";
    }
  }
  if (type === "goto" && !action.url && action.href) {
    action.url = action.href;
  }
  if (type === "type" && action.text === undefined && action.value !== undefined) {
    action.text = action.value;
  }
  if (type === "type" && action.text === undefined && action.input !== undefined) {
    action.text = action.input;
  }
  action.type = type;
  return action;
}

async function runAction(context, action, state) {
  action = normalizeActionDefaults(action);
  const type = String(action.type || "").trim().toLowerCase();
  const retryCount = Number.isFinite(action.retry_count) ? action.retry_count : state.retryCount;
  const timeoutMs = action.timeout_seconds ? action.timeout_seconds * 1000 : state.timeoutMs;

  return withRetry(type || "browser_action", retryCount, async () => {
    const page = await ensureCurrentPage(context, state);

    switch (type) {
      case "goto": {
        const response = await page.goto(action.url, {
          waitUntil: action.wait_until || "domcontentloaded",
          timeout: timeoutMs,
        });
        refreshTabState(context, state);
        return {
          type,
          url: page.url(),
          status: response ? response.status() : null,
          tab_index: state.currentTabIndex,
        };
      }
      case "observe": {
        return { type, snapshot: await observePage(page, action), tab_index: state.currentTabIndex };
      }
      case "wait_for": {
        const waited = await waitForAction(page, action, timeoutMs);
        return { type, waited, tab_index: state.currentTabIndex };
      }
      case "click": {
        const locator = await locateBySelectorOrText(page, action);
        if (!locator) {
          throw new Error("click action requires selector or text");
        }
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        if (action.wait_for_navigation) {
          await Promise.all([
            maybeWaitForLoadState(page, action.load_state || "domcontentloaded", timeoutMs),
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
        refreshTabState(context, state);
        return { type, selector: action.selector || "", text: action.text || "", url: page.url(), tab_index: state.currentTabIndex };
      }
      case "type": {
        const locator = await locateBySelectorOrText(page, action);
        if (!locator) {
          throw new Error("type action requires selector, text, label, placeholder, or role/name");
        }
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        if (action.clear !== false) {
          await locator.fill(String(action.text || ""), { timeout: timeoutMs });
        } else {
          await locator.type(String(action.text || ""), { timeout: timeoutMs });
        }
        return { type, selector: action.selector, text_length: String(action.text || "").length, tab_index: state.currentTabIndex };
      }
      case "press": {
        const locator = await locateBySelectorOrText(page, action);
        if (!locator) {
          throw new Error("press action requires selector, text, label, placeholder, or role/name");
        }
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        await locator.press(String(action.key || "Enter"), { timeout: timeoutMs });
        return { type, selector: action.selector, key: action.key || "Enter", tab_index: state.currentTabIndex };
      }
      case "login_form": {
        const usernameField = action.username_selector
          ? await locateBySelectorOrText(page, { selector: action.username_selector })
          : await firstVisibleLocator([
            page.locator("input[type='text'],input[type='email'],input[name*='user' i],input[name*='login' i],input[name*='email' i]"),
            page.getByLabel(/user|login|email|账号|用户名|邮箱/i),
            page.getByPlaceholder(/user|login|email|账号|用户名|邮箱/i),
          ]);
        const passwordField = action.password_selector
          ? await locateBySelectorOrText(page, { selector: action.password_selector })
          : await firstVisibleLocator([
            page.locator("input[type='password']"),
            page.getByLabel(/password|密码/i),
            page.getByPlaceholder(/password|密码/i),
          ]);
        if (!usernameField || !passwordField) {
          return { type, status: "form_not_found", snapshot: await observePage(page, { max_items: 30 }), tab_index: state.currentTabIndex };
        }
        await usernameField.fill(String(action.username || ""), { timeout: timeoutMs });
        await passwordField.fill(String(action.password || ""), { timeout: timeoutMs });
        if (action.submit_selector) {
          await page.locator(action.submit_selector).first().click({ timeout: timeoutMs });
        } else if (action.submit_text) {
          await page.getByText(action.submit_text, { exact: Boolean(action.submit_exact) }).first().click({ timeout: timeoutMs });
        } else {
          await passwordField.press("Enter", { timeout: timeoutMs });
        }
        if (action.wait_for_selector) {
          await page.locator(action.wait_for_selector).first().waitFor({ state: "visible", timeout: timeoutMs });
        } else if (action.wait_for_navigation !== false) {
          await maybeWaitForLoadState(page, action.load_state || "domcontentloaded", timeoutMs);
        }
        return { type, username: String(action.username || ""), url: page.url(), snapshot: await observePage(page, { max_items: 20, text_limit: 500 }), tab_index: state.currentTabIndex };
      }
      case "extract_table": {
        const table = await extractTable(page, action.selector);
        return { type, selector: action.selector, name: action.name || "", table, tab_index: state.currentTabIndex };
      }
      case "extract_text": {
        const locator = page.locator(action.selector).first();
        await locator.waitFor({ state: "visible", timeout: timeoutMs });
        const text = (await locator.textContent()) || "";
        return { type, selector: action.selector, text: text.trim(), tab_index: state.currentTabIndex };
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
        return { type, text: action.text, selector: action.selector || "", tab_index: state.currentTabIndex };
      }
      case "screenshot": {
        const target = resolveArtifactPath(state.artifactsDir, action.path || `screenshot-${Date.now()}.png`, ".png");
        await page.screenshot({
          path: target,
          fullPage: Boolean(action.full_page),
          timeout: timeoutMs,
        });
        return { type, path: target, tab_index: state.currentTabIndex };
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
        return { type, path: target, tab_index: state.currentTabIndex };
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
        return { type, download: item, tab_index: state.currentTabIndex };
      }
      case "list_downloads": {
        const files = listDownloadedFiles(state.artifactsDir);
        return { type, downloads: files };
      }
      case "open_tab": {
        let newPage = null;
        if (isNonEmptyString(action.url)) {
          newPage = await context.newPage();
          if (action.url) {
            await newPage.goto(action.url, {
              waitUntil: action.wait_until || "domcontentloaded",
              timeout: timeoutMs,
            });
          }
        } else if (action.selector || action.text) {
          const trigger = await locateBySelectorOrText(page, action);
          if (!trigger) {
            throw new Error("open_tab requires url, selector, or text");
          }
          const popupPromise = context.waitForEvent("page", { timeout: timeoutMs }).catch(() => null);
          await trigger.click({ timeout: timeoutMs });
          newPage = await popupPromise;
          if (!newPage) {
            throw new Error("open_tab did not create a new tab");
          }
          await maybeWaitForLoadState(newPage, action.load_state || "domcontentloaded", timeoutMs);
        } else {
          throw new Error("open_tab requires url, selector, or text");
        }
        if (action.switch !== false) {
          state.currentPage = newPage;
        }
        refreshTabState(context, state);
        return {
          type,
          opened_tab_index: state.pages.indexOf(newPage),
          current_tab_index: state.currentTabIndex,
          url: typeof newPage.url === "function" ? newPage.url() : "",
          tabs: await collectTabsInfo(context, state),
        };
      }
      case "switch_tab": {
        const targetIndex = await resolveTargetTabIndex(context, state, action);
        if (targetIndex < 0) {
          throw new Error("switch_tab could not find a matching tab");
        }
        state.currentPage = state.pages[targetIndex];
        if (typeof state.currentPage.bringToFront === "function") {
          await state.currentPage.bringToFront();
        }
        refreshTabState(context, state);
        return {
          type,
          current_tab_index: state.currentTabIndex,
          url: state.currentPage.url(),
          tabs: await collectTabsInfo(context, state),
        };
      }
      case "close_tab": {
        const targetIndex = await resolveTargetTabIndex(context, state, action);
        if (targetIndex < 0) {
          throw new Error("close_tab could not find a matching tab");
        }
        if (state.pages.length === 1 && action.allow_last !== true) {
          throw new Error("refusing to close the last tab without allow_last=true");
        }
        const closing = state.pages[targetIndex];
        const wasCurrent = targetIndex === state.currentTabIndex;
        await closing.close();
        refreshTabState(context, state);
        if (wasCurrent) {
          state.currentPage = state.pages[Math.min(targetIndex, Math.max(0, state.pages.length - 1))] || null;
          refreshTabState(context, state);
        }
        return {
          type,
          closed_tab_index: targetIndex,
          current_tab_index: state.currentTabIndex,
          tabs: await collectTabsInfo(context, state),
        };
      }
      case "list_tabs": {
        return {
          type,
          current_tab_index: state.currentTabIndex,
          tabs: await collectTabsInfo(context, state),
        };
      }
      case "upload_files": {
        if (!action.selector) {
          throw new Error("upload_files requires selector");
        }
        const files = normalizeUploadFiles(action);
        const locator = page.locator(action.selector).first();
        await locator.waitFor({ state: "attached", timeout: timeoutMs });
        await locator.setInputFiles(files);
        return {
          type,
          selector: action.selector,
          count: files.length,
          tab_index: state.currentTabIndex,
        };
      }
      case "fill_form": {
        const entries = normalizeFormEntries(action.fields);
        if (entries.length === 0) {
          throw new Error("fill_form requires fields");
        }
        const filled = [];
        for (const entry of entries) {
          const locator = await resolveFormField(page, entry.name);
          filled.push(await applyFieldValue(page, locator, entry.name, entry.value, timeoutMs));
        }
        const submitted = await triggerSubmit(page, action, timeoutMs);
        if (submitted && action.wait_for_navigation) {
          await maybeWaitForLoadState(page, action.load_state || "domcontentloaded", timeoutMs);
        }
        if (action.wait_for_selector) {
          await page.locator(action.wait_for_selector).first().waitFor({ state: "visible", timeout: timeoutMs });
        }
        return {
          type,
          filled,
          submitted,
          url: page.url(),
          tab_index: state.currentTabIndex,
        };
      }
      case "paginate_extract": {
        if (!action.item_selector) {
          throw new Error("paginate_extract requires item_selector");
        }
        const maxPages = Number.isFinite(action.max_pages) && action.max_pages > 0 ? action.max_pages : 5;
        const uniqueBy = isNonEmptyString(action.unique_by) ? action.unique_by : "";
        const seen = new Set();
        const items = [];
        let pagesVisited = 0;

        for (let index = 0; index < maxPages; index += 1) {
          if (action.wait_for_selector) {
            await page.locator(action.wait_for_selector).first().waitFor({ state: "visible", timeout: timeoutMs });
          }
          const pageItems = await extractPaginatedItems(page, action);
          for (const item of pageItems) {
            if (action.include_page_url) {
              item.page_url = page.url();
            }
            const dedupeKey = uniqueBy && item[uniqueBy] !== undefined ? String(item[uniqueBy]) : JSON.stringify(item);
            if (seen.has(dedupeKey)) {
              continue;
            }
            seen.add(dedupeKey);
            items.push(item);
          }
          pagesVisited += 1;

          const nextLocator = await resolvePaginationNextLocator(page, action);
          if (!nextLocator || !await locatorExists(nextLocator)) {
            break;
          }
          if (await isLocatorDisabled(nextLocator)) {
            break;
          }

          const previousUrl = page.url();
          await Promise.all([
            maybeWaitForLoadState(page, action.load_state || "domcontentloaded", timeoutMs),
            nextLocator.click({ timeout: timeoutMs }),
          ]);

          if (action.stop_on_repeated_url !== false && previousUrl === page.url() && !action.next_selector && !action.next_text) {
            break;
          }
        }
        return {
          type,
          pages_visited: pagesVisited,
          total_items: items.length,
          items,
          tab_index: state.currentTabIndex,
        };
      }
      case "check_login_state": {
        if (isNonEmptyString(action.goto_url)) {
          await page.goto(action.goto_url, {
            waitUntil: action.wait_until || "domcontentloaded",
            timeout: timeoutMs,
          });
        }
        const probeTimeoutMs = action.probe_timeout_seconds ? action.probe_timeout_seconds * 1000 : Math.min(timeoutMs, 2500);
        let status = await detectLoginState(page, action, probeTimeoutMs);
        let recovered = null;
        if (status === "logged_out" && action.recover_with_login_form && typeof action.recover_with_login_form === "object") {
          recovered = await runAction(context, {
            type: "login_form",
            ...action.recover_with_login_form,
          }, state);
          status = await detectLoginState(page, action, probeTimeoutMs);
        }
        if (status !== "logged_in" && isNonEmptyString(action.failure_screenshot_path)) {
          const target = resolveArtifactPath(state.artifactsDir, action.failure_screenshot_path, ".png");
          await page.screenshot({ path: target, fullPage: true });
        }
        if (status !== "logged_in" && action.require_authenticated === true) {
          throw new Error(`login state check failed: ${status}`);
        }
        return {
          type,
          status,
          authenticated: status === "logged_in",
          recovered,
          url: page.url(),
          tab_index: state.currentTabIndex,
        };
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
    timeoutMs: qorvexus.actionTimeoutSeconds * 1000,
    currentPage: page,
    pages: context.pages(),
    currentTabIndex: 0,
  };
  refreshTabState(context, state);
  const results = [];
  if (input.start_url) {
    results.push(await runAction(context, { type: "goto", url: input.start_url, wait_until: "domcontentloaded" }, state));
  }
  for (const action of actions) {
    results.push(await runAction(context, action, state));
  }
  const currentPage = await ensureCurrentPage(context, state);
  return {
    mode: "actions",
    current_url: currentPage.url(),
    current_tab_index: state.currentTabIndex,
    tabs: await collectTabsInfo(context, state),
    downloads: listDownloadedFiles(qorvexus.artifactsDir),
    snapshot: await observePage(currentPage, { max_items: 40, text_limit: 1200 }),
    results,
  };
}

async function runScriptMode(page, context, playwright, browserType, browser, qorvexus) {
  const scriptPath = process.env.QORVEXUS_PLAYWRIGHT_SCRIPT_FILE;
  if (!scriptPath) {
    throw new Error("QORVEXUS_PLAYWRIGHT_SCRIPT_FILE is required in script mode");
  }
  const script = fs.readFileSync(scriptPath, "utf8");
  const browserForScript = browser || {
    async newContext() {
      return context;
    },
    contexts() {
      return [context];
    },
    isConnected() {
      return true;
    },
    async close() {
    },
  };
  Object.assign(globalThis, {
    playwright,
    chromium: playwright.chromium,
    firefox: playwright.firefox,
    webkit: playwright.webkit,
    browserType,
    browser: browserForScript,
    context,
    page,
    qorvexus,
  });
  const wrapped = new Function(`return (async () => {\n${script}\n})();`);
  return wrapped();
}

async function main() {
  const mode = (process.env.QORVEXUS_PLAYWRIGHT_MODE || "script").trim().toLowerCase();
  const browserName = String(process.env.QORVEXUS_PLAYWRIGHT_BROWSER || "chromium").trim().toLowerCase() || "chromium";
  const profileDir = process.env.QORVEXUS_PLAYWRIGHT_PROFILE_DIR || "";
  const storageStatePath = process.env.QORVEXUS_PLAYWRIGHT_STORAGE_STATE_FILE || "";
  const artifactsDir = process.env.QORVEXUS_PLAYWRIGHT_ARTIFACTS_DIR || "";
  const persistProfile = envBool("QORVEXUS_PLAYWRIGHT_PERSIST_PROFILE", true);
  const saveStorageState = envBool("QORVEXUS_PLAYWRIGHT_SAVE_STORAGE_STATE", true);
  const headless = envBool("QORVEXUS_PLAYWRIGHT_HEADLESS", true);
  const timeoutSeconds = envInt("QORVEXUS_PLAYWRIGHT_TIMEOUT_SECONDS", 120);
  const actionTimeoutSeconds = envInt("QORVEXUS_PLAYWRIGHT_ACTION_TIMEOUT_SECONDS", Math.min(timeoutSeconds, 10));

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
    actionTimeoutSeconds,
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
    async observe(options) {
      return observePage(await this.ensurePage(), options || {});
    },
    async click(target, options) {
      const pageForAction = await this.ensurePage();
      const locator = await locateBySelectorOrText(pageForAction, typeof target === "string" ? { selector: target, text: target, ...(options || {}) } : (target || {}));
      if (!locator) {
        throw new Error("click helper could not find target");
      }
      await locator.click({ timeout: actionTimeoutSeconds * 1000 });
    },
    async fill(target, value, options) {
      const pageForAction = await this.ensurePage();
      const locator = await locateBySelectorOrText(pageForAction, typeof target === "string" ? { selector: target, label: target, placeholder: target, ...(options || {}) } : (target || {}));
      if (!locator) {
        throw new Error("fill helper could not find target");
      }
      await locator.fill(String(value || ""), { timeout: actionTimeoutSeconds * 1000 });
    },
    withRetry,
  };

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

    context.setDefaultTimeout(actionTimeoutSeconds * 1000);
    if (context.pages().length > 0) {
      page = context.pages()[0];
    } else {
      page = await context.newPage();
    }

    let result;
    if (mode === "actions") {
      result = await withOverallTimeout(runActionsMode(page, context, qorvexus), timeoutSeconds * 1000, "Playwright run");
    } else {
      result = await withOverallTimeout(runScriptMode(page, context, playwright, browserType, browser, qorvexus), timeoutSeconds * 1000, "Playwright run");
      if (result === undefined) {
        result = { mode: "script", snapshot: await observePage(page, { max_items: 40, text_limit: 1200 }) };
      }
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
    if (context && saveStorageState && storageStatePath) {
      await context.storageState({ path: storageStatePath }).catch(() => {});
    }
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
