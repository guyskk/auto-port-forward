#!/usr/bin/env bash
set -euo pipefail

go run github.com/wailsapp/wails/v2/cmd/wails build -clean "$@"
