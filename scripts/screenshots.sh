#!/bin/sh
# Regenerates docs/screenshot.png and docs/screenshot-light.png from the UI
# tests' own fixtures, rendered headlessly. Run it after a visible change.
set -eu
cd "$(dirname "$0")/.."
YARM_SCREENSHOTS="$PWD/docs" go test ./internal/ui -run TestScreenshots -count=1
echo "wrote docs/screenshot.png and docs/screenshot-light.png"
