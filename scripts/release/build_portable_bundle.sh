#!/usr/bin/env bash
set -euo pipefail

target="${TARGET:?TARGET is required}"
playwright_version="${PLAYWRIGHT_VERSION:-1.48.2}"

bundle="dist/qorvexus-${target}"
release_dir="release"
rm -rf "${bundle}"
mkdir -p "${bundle}/bin" "${bundle}/source" "${bundle}/runtimes" "${release_dir}"

go build -trimpath -ldflags="-s -w" -o "${bundle}/bin/qorvexus" ./cmd/qorvexus

git archive --format=tar HEAD | tar -x -C "${bundle}/source"

go_root="$(go env GOROOT)"
cp -R "${go_root}" "${bundle}/runtimes/go"

node_root="$(node -p 'require("path").dirname(require("path").dirname(process.execPath))')"
cp -R "${node_root}" "${bundle}/runtimes/node"

playwright_runtime="${bundle}/runtimes/playwright"
playwright_browsers="${bundle}/runtimes/playwright-browsers"
mkdir -p "${playwright_runtime}" "${playwright_browsers}"
cat > "${playwright_runtime}/package.json" <<JSON
{
  "name": "qorvexus-portable-playwright-runtime",
  "private": true,
  "dependencies": {
    "playwright": "${playwright_version}"
  }
}
JSON
npm install --prefix "${playwright_runtime}" --no-fund --no-audit "playwright@${playwright_version}"
PLAYWRIGHT_BROWSERS_PATH="${playwright_browsers}" \
  node "${playwright_runtime}/node_modules/playwright/cli.js" install chromium
printf '%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "${playwright_runtime}/.chromium.ready"

cp scripts/portable/run.sh "${bundle}/run.sh"
chmod +x "${bundle}/run.sh"

cat > "${bundle}/README-PORTABLE.md" <<EOF
# Qorvexus Portable Bundle (${target})

This bundle includes:

- Qorvexus binary
- Full Qorvexus source tree
- Go toolchain
- Node.js and npm
- Playwright npm package
- Playwright Chromium browser cache

Start the supervised runtime:

\`\`\`sh
./run.sh
\`\`\`

Run any Qorvexus command through the bundled environment:

\`\`\`sh
./run.sh run "hello"
./run.sh skills
\`\`\`

The launcher sets \`QORVEXUS_SOURCE_ROOT\`, \`QORVEXUS_PLAYWRIGHT_RUNTIME_DIR\`,
\`PLAYWRIGHT_BROWSERS_PATH\`, \`GOROOT\`, and \`PATH\` so self-update builds use the
included source tree and Go toolchain.
EOF

tar -C dist -czf "${release_dir}/qorvexus-${target}.tar.gz" "qorvexus-${target}"
