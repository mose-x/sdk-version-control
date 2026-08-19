#!/usr/bin/env bash
# Build the Linux bare binary, .deb, and .rpm for SDK Version Control.
#
# Usage:   ./scripts/build-linux.sh <version> [arch]
#   version:  release version, e.g. 1.1.0
#   arch:     amd64 | arm64  (default: native GOARCH)
#
# Produces in build/bin/:
#   SDKVersionControl-<ver>-linux-<x64|arm64>          (self-update)
#   SDKVersionControl-<ver>-linux-<x64|arm64>.deb      (first-install)
#   SDKVersionControl-<ver>-linux-<x64|arm64>.rpm
#
# Prereqs: Go 1.25+, Node 18+, Wails CLI, jq, fpm (ruby gem),
#   libgtk-3-dev libwebkit2gtk-4.0-dev libayatana-appindicator3-dev librsvg2-dev,
#   python3-pil. Wails v2 targets webkit2gtk-4.0 (ubuntu-22.04/jammy); 4.1 is
#   Wails v3 only. Linux ARM64 cannot be cross-compiled (webkit2gtk CGO), so run
#   this script on a native arm64 host for arm64.
#
# This script is the single source of truth for the Linux packaging steps;
# .github/workflows/build.yml calls it so CI and local builds produce identical
# artifacts.
set -euo pipefail

VERSION="${1:?version required, e.g. 1.1.0}"
ARCH="${2:-$(go env GOARCH)}"
case "$ARCH" in
  amd64) ASSET_ARCH="x64" ;;
  arm64) ASSET_ARCH="arm64" ;;
  *) echo "unsupported arch: $ARCH (want amd64|arm64)" >&2; exit 1 ;;
esac
DEB_ARCH="$ARCH"
if [ "$ARCH" = "amd64" ]; then
  RPM_ARCH="x86_64"
else
  RPM_ARCH="aarch64"
fi

# --- System dependencies (idempotent; safe to re-run). Requires sudo.
if ! pkg-config --exists webkit2gtk-4.0 2>/dev/null; then
  echo "Installing system dependencies (requires sudo)..."
  sudo apt-get update
  sudo apt-get install -y \
    libgtk-3-dev libwebkit2gtk-4.0-dev \
    libayatana-appindicator3-dev librsvg2-dev \
    ruby ruby-dev python3-pil
fi
command -v fpm >/dev/null 2>&1 || sudo gem install fpm

# --- Bump about.json so the binary reports the correct version to CheckUpdate.
jq --arg v "$VERSION" '.version = $v' about.json > about.json.tmp && mv about.json.tmp about.json
echo "about.json version bumped to $VERSION"

# --- Build the bare binary.
wails build -platform "linux/$ARCH" -o SDKVersionControl
# Wails appends the arch suffix when cross-compiling; normalize to the plain
# name first, then rename to the final asset name.
# R2: Use mv -f to always replace, no conditional that skips on stale presence.
mv -f "build/bin/SDKVersionControl-$ARCH" "build/bin/SDKVersionControl"
ASSET_NAME="SDKVersionControl-${VERSION}-linux-${ASSET_ARCH}"
mv "build/bin/SDKVersionControl" "build/bin/${ASSET_NAME}"

# --- Build .deb and .rpm with a .desktop entry + hicolor icons so the app
# shows up in application launchers with a proper icon.
DEB_NAME="SDKVersionControl-${VERSION}-linux-${ASSET_ARCH}.deb"
RPM_NAME="SDKVersionControl-${VERSION}-linux-${ASSET_ARCH}.rpm"
STAGING="$(mktemp -d)"
mkdir -p "$STAGING/share/applications"
cat > "$STAGING/share/applications/sdkversioncontrol.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=SDK Version Control
Comment=Manage SDK versions in one place
Exec=/usr/bin/SDKVersionControl
Icon=sdkversioncontrol
Terminal=false
Categories=Development;
DESKTOP
SRC_ICON="build/desktop-icons/icon-white-bg.png"
for sz in 16 32 48 64 128 256 512; do
  d="$STAGING/share/icons/hicolor/${sz}x${sz}/apps"
  mkdir -p "$d"
  python3 -c "from PIL import Image; Image.open('$SRC_ICON').resize(($sz,$sz), Image.LANCZOS).save('$d/sdkversioncontrol.png')"
done

fpm -s dir -t deb \
  -n SDKVersionControl -v "${VERSION}" -a "${DEB_ARCH}" \
  --depends libgtk-3-0 \
  --depends libwebkit2gtk-4.0-37 \
  --depends libayatana-appindicator3-1 \
  --depends librsvg2-2 \
  --description "SDK Version Control" \
  -p "build/bin/${DEB_NAME}" \
  "build/bin/${ASSET_NAME}=/usr/bin/SDKVersionControl" \
  "$STAGING/share/=/usr/share/"

fpm -s dir -t rpm \
  -n SDKVersionControl -v "${VERSION}" -a "${RPM_ARCH}" \
  --depends gtk3 \
  --depends webkit2gtk3 \
  --depends libayatana-appindicator3 \
  --depends librsvg2 \
  --description "SDK Version Control" \
  -p "build/bin/${RPM_NAME}" \
  "build/bin/${ASSET_NAME}=/usr/bin/SDKVersionControl" \
  "$STAGING/share/=/usr/share/"

echo
echo "Built:"
echo "  build/bin/${ASSET_NAME}"
echo "  build/bin/${DEB_NAME}"
echo "  build/bin/${RPM_NAME}"
