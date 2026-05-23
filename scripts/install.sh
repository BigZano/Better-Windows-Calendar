#!/usr/bin/env bash
# One-line installer for PyCalendar on Linux (amd64).
# Usage:
#   curl -fsSL https://github.com/BigZano/Better-Windows-Calendar/releases/latest/download/install.sh | bash
#   curl -fsSL https://github.com/BigZano/Better-Windows-Calendar/releases/download/v1.2.3/install.sh | bash
set -euo pipefail

REPO="BigZano/Better-Windows-Calendar"
BINARY="pycalendar"
INSTALL_DIR="/usr/local/bin"
DESKTOP_DIR="/usr/share/applications"

# Resolve latest release tag if VERSION not set externally.
VERSION="${PYCALENDAR_VERSION:-}"
if [[ -z "$VERSION" ]]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
        | grep '"tag_name"' | head -1 | sed 's/.*"v\([^"]*\)".*/\1/')
fi
if [[ -z "$VERSION" ]]; then
    echo "error: could not determine latest version" >&2
    exit 1
fi

ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

BASE_URL="https://github.com/$REPO/releases/download/v$VERSION"
ARCHIVE="pycalendar-$VERSION-linux-$ARCH.tar.gz"

echo "Downloading PyCalendar v$VERSION ($ARCH)..."
curl -fsSL "$BASE_URL/$ARCHIVE" -o "$TMP/$ARCHIVE"
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"

SRC="$TMP/pycalendar-$VERSION-linux-$ARCH"

echo "Installing to $INSTALL_DIR/$BINARY (may prompt for sudo)..."
sudo install -Dm755 "$SRC/$BINARY" "$INSTALL_DIR/$BINARY"

if [[ -f "$SRC/pycalendar.desktop" ]]; then
    sudo install -Dm644 "$SRC/pycalendar.desktop" "$DESKTOP_DIR/pycalendar.desktop"
    command -v update-desktop-database &>/dev/null && sudo update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
fi

echo ""
echo "PyCalendar v$VERSION installed."
echo "  Run:       pycalendar"
echo "  Tray mode: pycalendar --mode tray"
echo "  Uninstall: sudo rm $INSTALL_DIR/$BINARY $DESKTOP_DIR/pycalendar.desktop"
