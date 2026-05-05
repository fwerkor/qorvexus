$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path

$env:QORVEXUS_SOURCE_ROOT = Join-Path $Root "source"
$env:QORVEXUS_PLAYWRIGHT_RUNTIME_DIR = Join-Path $Root "runtimes\playwright"
$env:PLAYWRIGHT_BROWSERS_PATH = Join-Path $Root "runtimes\playwright-browsers"
$env:GOROOT = Join-Path $Root "runtimes\go"

$GoBin = Join-Path $env:GOROOT "bin"
$NodeRoot = Join-Path $Root "runtimes\node"
$NodeNpmBin = Join-Path $NodeRoot "node_modules\npm\bin"
$env:PATH = "$GoBin;$NodeRoot;$NodeNpmBin;$env:PATH"

Set-Location $env:QORVEXUS_SOURCE_ROOT

$Qorvexus = Join-Path $Root "bin\qorvexus.exe"
if ($args.Count -eq 0) {
  & $Qorvexus start
} else {
  & $Qorvexus @args
}
exit $LASTEXITCODE
