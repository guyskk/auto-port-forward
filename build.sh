#!/usr/bin/env bash
set -euo pipefail

WAILS_VERSION="v2.12.0"

if ! command -v wails >/dev/null 2>&1; then
  echo "wails CLI not found, installing ${WAILS_VERSION}..."
  (cd "$(mktemp -d)" && go install "github.com/wailsapp/wails/v2/cmd/wails@${WAILS_VERSION}")
fi

wails build -clean "$@"
