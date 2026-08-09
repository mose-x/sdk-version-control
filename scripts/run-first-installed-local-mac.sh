#!/usr/bin/env bash
#
# run-first-installed-local-mac.sh
# 本地构建的 macOS DMG 首次启动修复脚本
# First-launch fix script for locally-built macOS DMG installs
#
# 什么时候用这个脚本 / When to use this script
# ==========================================================
#
# 当你从本地构建的 DMG 安装了 SDK Version Control 后,macOS 可能会因为
# 签名和隔离属性的问题阻止应用启动(表现为"已损坏,无法打开"或进程
# 被 SIGKILL 137 杀掉)。这在严格 MDM 管控的 Mac 上尤为常见。
#
# After installing SDK Version Control from a locally-built DMG, macOS may
# block the app from launching due to ad-hoc signature + quarantine
# attributes. You'll see "damaged and can't be opened" or the process
# gets SIGKILL'd (exit 137). This is especially common on strict MDM
# managed Macs.
#
# 这个脚本做四件事 / This script does four things:
#   1. 去掉 .app bundle 的 ad-hoc 签名 / Strip ad-hoc signature from .app bundle
#   2. 去掉内层二进制的 ad-hoc 签名 / Strip ad-hoc signature from inner binary
#   3. 清除所有 xattr(含浏览器下载的 quarantine)/ Clear all xattrs
#   4. 重新打一个干净的 quarantine(走 Gatekeeper 旁路)/ Re-stamp fresh quarantine
#
# 用法 / Usage:
#   chmod +x run-first-installed-local-mac.sh
#   ./run-first-installed-local-mac.sh
#
# 前提 / Prerequisites:
#   - SDK Version Control 已从 DMG 拖到 /Applications
#     SDK Version Control has been dragged to /Applications from the DMG
#   - 在 macOS 上运行(Run on macOS)
#
# 注意 / Note:
#   - 只需首次运行一次。之后直接双击 .app 即可。
#     Only run once on first install. After that, double-click the .app directly.
#   - 如果从 CI release 浏览器下载的 DMG 安装,通常不需要此脚本
#     (浏览器会自动加正确的 quarantine)。
#     If installing from a CI release DMG downloaded via browser, this script
#     is usually not needed (the browser adds correct quarantine automatically).
#   - 此脚本需要 codesign 和 xattr 命令(macOS 自带)。
#     Requires codesign and xattr commands (bundled with macOS).
#

set -euo pipefail

APP="/Applications/SDK Version Control.app"

if [ ! -d "$APP" ]; then
    echo "错误: $APP 不存在。请先从 DMG 拖到 /Applications。"
    echo "Error: $APP not found. Please drag the app to /Applications from the DMG first."
    exit 1
fi

echo "1/4 去掉 bundle 签名 / Stripping bundle signature..."
codesign --remove-signature "$APP" 2>/dev/null || true

echo "2/4 去掉内层二进制签名 / Stripping inner binary signature..."
codesign --remove-signature "$APP/Contents/MacOS/SDKVersionControl" 2>/dev/null || true

echo "3/4 清除所有 xattr / Clearing all xattrs..."
xattr -cr "$APP"

echo "4/4 重新打 quarantine / Re-stamping quarantine..."
xattr -w com.apple.quarantine "0083;00000000;SVC;|com.mose-x.sdkversioncontrol" "$APP"

echo ""
echo "完成!正在启动应用... / Done! Launching app..."
open "$APP"
