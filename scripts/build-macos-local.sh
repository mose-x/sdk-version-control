#!/usr/bin/env bash
# Local MDM-Mac build wrapper for svc.
#
# Usage:   ./scripts/build-macos-local.sh <version> [arch]
#   version:  release version, e.g. 1.1.1
#   arch:     amd64 | arm64  (default: native GOARCH)
#
# Identical to scripts/build-macos.sh but sets SVC_SKIP_BINDINGS=1 so Wails
# skips binding generation. Under strict MDM, `wails build` runs an
# ad-hoc-signed binding-generator binary that amfid kills on the provenance
# path (no quarantine, local build); -skipbindings avoids that run so the build
# completes on an MDM-Mac.
#
# Caveat: relies on the committed frontend/wailsjs/ bindings being current.
# Before changing Go App methods exposed to the frontend, run a non-skip
# `wails build` (on a non-MDM machine or CI) to regenerate + commit the
# bindings, otherwise this script builds against stale bindings.
#
# The bundle's ad-hoc signature is still stripped (right-click -> Open bypass)
# and the DMG is still stamped with com.apple.quarantine -- both handled by
# build-macos.sh. Run on macOS only (delegates to macOS-only tools).
set -euo pipefail
exec env SVC_SKIP_BINDINGS=1 "$(dirname "$0")/build-macos.sh" "$@"
