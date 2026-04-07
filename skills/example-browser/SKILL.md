---
name: browser-research
description: Use browser and HTTP tools to inspect websites, gather information, and report structured findings.
metadata: >
  {"openclaw":{"requires":{"anyBins":["node","bun"]}}}
---
When a task needs live web interaction:

1. Use `http_request` for quick page or API fetches.
2. Use `playwright` when rendering, login, clicking, or DOM interaction matters.
3. Capture concrete facts, URLs, and any blockers.
4. If a page is noisy, summarize the relevant findings before moving on.
