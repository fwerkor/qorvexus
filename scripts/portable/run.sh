#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

export QORVEXUS_SOURCE_ROOT="${ROOT}/source"
export QORVEXUS_PLAYWRIGHT_RUNTIME_DIR="${ROOT}/runtimes/playwright"
export PLAYWRIGHT_BROWSERS_PATH="${ROOT}/runtimes/playwright-browsers"
export GOROOT="${ROOT}/runtimes/go"
export PATH="${ROOT}/runtimes/go/bin:${ROOT}/runtimes/node/bin:${PATH}"

cd "${QORVEXUS_SOURCE_ROOT}"

if [ "$#" -eq 0 ]; then
	exec "${ROOT}/bin/qorvexus" start
fi

exec "${ROOT}/bin/qorvexus" "$@"
