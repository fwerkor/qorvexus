$ErrorActionPreference = "Stop"

if (-not $env:TARGET) {
  throw "TARGET is required"
}

$Target = $env:TARGET
$PlaywrightVersion = if ($env:PLAYWRIGHT_VERSION) { $env:PLAYWRIGHT_VERSION } else { "1.48.2" }

$Bundle = Join-Path "dist" "qorvexus-$Target"
$ReleaseDir = "release"
Remove-Item -Recurse -Force $Bundle -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path "$Bundle\bin", "$Bundle\source", "$Bundle\runtimes", $ReleaseDir | Out-Null

go build -trimpath -ldflags="-s -w" -o "$Bundle\bin\qorvexus.exe" ./cmd/qorvexus
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

git archive --format=tar HEAD | tar -x -C "$Bundle\source"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$GoRoot = go env GOROOT
Copy-Item -Recurse -Force $GoRoot "$Bundle\runtimes\go"

$NodeRoot = node -p "require('path').dirname(process.execPath)"
Copy-Item -Recurse -Force $NodeRoot "$Bundle\runtimes\node"

$PlaywrightRuntime = Join-Path $Bundle "runtimes\playwright"
$PlaywrightBrowsers = Join-Path $Bundle "runtimes\playwright-browsers"
New-Item -ItemType Directory -Force -Path $PlaywrightRuntime, $PlaywrightBrowsers | Out-Null
@"
{
  "name": "qorvexus-portable-playwright-runtime",
  "private": true,
  "dependencies": {
    "playwright": "$PlaywrightVersion"
  }
}
"@ | Set-Content -NoNewline -Encoding UTF8 "$PlaywrightRuntime\package.json"

npm install --prefix "$PlaywrightRuntime" --no-fund --no-audit "playwright@$PlaywrightVersion"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$env:PLAYWRIGHT_BROWSERS_PATH = $PlaywrightBrowsers
node "$PlaywrightRuntime\node_modules\playwright\cli.js" install chromium
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

(Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ") | Set-Content -NoNewline -Encoding ASCII "$PlaywrightRuntime\.chromium.ready"

Copy-Item -Force "scripts\portable\run.ps1" "$Bundle\run.ps1"

@"
# Qorvexus Portable Bundle ($Target)

This bundle includes:

- Qorvexus binary
- Full Qorvexus source tree
- Go toolchain
- Node.js and npm
- Playwright npm package
- Playwright Chromium browser cache

Start the supervised runtime:

````powershell
.\run.ps1
````

Run any Qorvexus command through the bundled environment:

````powershell
.\run.ps1 run "hello"
.\run.ps1 skills
````

The launcher sets ``QORVEXUS_SOURCE_ROOT``, ``QORVEXUS_PLAYWRIGHT_RUNTIME_DIR``,
``PLAYWRIGHT_BROWSERS_PATH``, ``GOROOT``, and ``PATH`` so self-update builds use the
included source tree and Go toolchain.
"@ | Set-Content -Encoding UTF8 "$Bundle\README-PORTABLE.md"

Compress-Archive -Force -Path $Bundle -DestinationPath "$ReleaseDir\qorvexus-$Target.zip"
