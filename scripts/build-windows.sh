#!/usr/bin/env bash
# Build the Windows .exe (self-update) and NSIS installer for SDK Version Control.
#
# Usage:   ./scripts/build-windows.sh <version> [arch]
#   version:  release version, e.g. 1.1.0
#   arch:     amd64 | arm64  (default: native GOARCH)
#
# Produces in build/bin/:
#   SDKVersionControl-<ver>-windows-<x64|arm64>.exe            (self-update)
#   SDKVersionControl-<ver>-windows-<x64|arm64>-installer.exe  (first-install)
#
# Prereqs: Go 1.25+, Node 18+, Wails CLI, jq, go-winres, NSIS (makensis).
# Wails Windows builds are CGO-free (WebView2 loaded at runtime via COM), so
# arm64 can be cross-compiled on an amd64 host.
#
# This script is the single source of truth for the Windows packaging steps;
# .github/workflows/build.yml calls it so CI and local builds produce identical
# artifacts. Run from Git Bash on Windows (the CI uses bash on windows-latest).
set -euo pipefail

VERSION="${1:?version required, e.g. 1.1.0}"
ARCH="${2:-$(go env GOARCH)}"
case "$ARCH" in
  amd64) ASSET_ARCH="x64" ;;
  arm64) ASSET_ARCH="arm64" ;;
  *) echo "unsupported arch: $ARCH (want amd64|arm64)" >&2; exit 1 ;;
esac

# --- Bump about.json so the binary reports the correct version to CheckUpdate.
jq --arg v "$VERSION" '.version = $v' about.json > about.json.tmp && mv about.json.tmp about.json
echo "about.json version bumped to $VERSION"

# --- Generate Windows resources (icon + manifest) -> resource.syso.
# go-winres --out resource produces a file named `resource` (no extension) with
# --no-suffix; rename it to resource.syso so the Go linker embeds it.
go-winres make --arch "$ARCH" --out resource --no-suffix
if [ -e resource ] && [ ! -e resource.syso ]; then
  mv resource resource.syso
fi

# --- makensis (NSIS) must be on PATH for `wails build -nsis` to produce the
# installer. On CI, the build.yml "Setup NSIS" step adds it to GITHUB_PATH
# (Test-Path + choco fallback). Locally, install NSIS and ensure makensis is on
# PATH. If makensis is absent, wails skips the installer and the guard below
# ships the bare .exe (self-update asset) without failing the build.
command -v makensis >/dev/null 2>&1 && makensis /VERSION || echo "warning: makensis not on PATH; installer will be skipped" >&2

# --- Build the console-subsystem svc-shim binary that the app //go:embeds.
# This MUST run before `wails build` so the bytes are captured at compile time.
# The committed file is an empty placeholder (so dev `wails build` works without
# this step); this overwrites it with the real binary and is never committed.
GOOS=windows GOARCH="$ARCH" go build -o internal/shimmanager/svc-shim.windows.exe ./cmd/svc-shim
echo "Built console shim binary:"
ls -la internal/shimmanager/svc-shim.windows.exe

# --- Build the app + NSIS installer.
# -nsis produces the installer alongside the bare .exe; the bare .exe stays the
# self-update asset (ApplyUpdate swaps just the executable).
wails build -nsis -nopackage -platform "windows/$ARCH" -o SDKVersionControl.exe

# Wails appends the arch suffix when cross-compiling; normalize to the plain
# name first, then rename to the final asset name so amd64/arm64 don't clobber.
if [ -f "build/bin/SDKVersionControl-$ARCH.exe" ] && [ ! -f "build/bin/SDKVersionControl.exe" ]; then
  mv "build/bin/SDKVersionControl-$ARCH.exe" "build/bin/SDKVersionControl.exe"
fi
ASSET_NAME="SDKVersionControl-${VERSION}-windows-${ASSET_ARCH}.exe"
mv "build/bin/SDKVersionControl.exe" "build/bin/${ASSET_NAME}"

# NSIS writes *-installer.exe next to the binary; the literal name varies with
# INFO_PRODUCTNAME (wails.json "name" has spaces), so glob to a stable name.
# Guard: if makensis was missing, wails produced no installer -- ship the bare
# .exe (self-update asset) without failing the build.
INSTALLER_NAME="SDKVersionControl-${VERSION}-windows-${ASSET_ARCH}-installer.exe"
shopt -s nullglob
installers=(build/bin/*-installer.exe)
shopt -u nullglob
if [ ${#installers[@]} -gt 0 ]; then
  mv "${installers[0]}" "build/bin/${INSTALLER_NAME}"
else
  echo "warning: no NSIS installer produced (makensis missing); shipping bare .exe only" >&2
fi

echo
echo "Built:"
echo "  build/bin/${ASSET_NAME}"
echo "  build/bin/${INSTALLER_NAME}"
