#!/usr/bin/env bash
# Build PyCalendar for Linux and create a .deb package + tar.gz archive.
# Usage: ./scripts/build-linux.sh [VERSION]
set -euo pipefail

VERSION="${1:-0.1.0}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
DEB_STAGE="$DIST/deb-stage"

mkdir -p "$DIST"

echo "Building pycalendar v$VERSION for linux/amd64..."
GOARCH=amd64 GOOS=linux \
  go build -ldflags="-s -w -X main.version=$VERSION" \
  -o "$DIST/pycalendar" "$ROOT"

echo "  -> dist/pycalendar"

# ---- tar.gz archive ----
ARCHIVE_DIR="$DIST/pycalendar-$VERSION-linux-amd64"
mkdir -p "$ARCHIVE_DIR"
cp "$DIST/pycalendar" "$ARCHIVE_DIR/"
cp "$ROOT/installer/linux/pycalendar.desktop" "$ARCHIVE_DIR/"
cat > "$ARCHIVE_DIR/install.sh" <<'INSTALL'
#!/usr/bin/env bash
set -e
sudo install -Dm755 pycalendar /usr/local/bin/pycalendar
sudo install -Dm644 pycalendar.desktop /usr/share/applications/pycalendar.desktop
echo "Installed. Run: pycalendar"
INSTALL
chmod +x "$ARCHIVE_DIR/install.sh"
tar -czf "$DIST/pycalendar-$VERSION-linux-amd64.tar.gz" -C "$DIST" "pycalendar-$VERSION-linux-amd64"
rm -rf "$ARCHIVE_DIR"
echo "  -> dist/pycalendar-$VERSION-linux-amd64.tar.gz"

# ---- .deb package ----
rm -rf "$DEB_STAGE"
install -Dm755 "$DIST/pycalendar"                            "$DEB_STAGE/usr/local/bin/pycalendar"
install -Dm644 "$ROOT/installer/linux/pycalendar.desktop"    "$DEB_STAGE/usr/share/applications/pycalendar.desktop"

# Copy icon if present
if [[ -f "$ROOT/assets/icon.png" ]]; then
    install -Dm644 "$ROOT/assets/icon.png" "$DEB_STAGE/usr/share/pixmaps/pycalendar.png"
fi

mkdir -p "$DEB_STAGE/DEBIAN"
cat > "$DEB_STAGE/DEBIAN/control" <<CONTROL
Package: pycalendar
Version: $VERSION
Section: utils
Priority: optional
Architecture: amd64
Depends: libgl1, libx11-6, libxcursor1, libxrandr2, libxinerama1, libxi6
Maintainer: Bret Zanotelli <bretzanotelli@yahoo.com>
Homepage: https://github.com/BigZano/Better-Windows-Calendar
Description: Desktop calendar app — no Microsoft account required
 PyCalendar is a lightweight system-tray calendar for Windows and Linux.
 Supports recurring events, categories, attachments, CalDAV sync (planned),
 and status-bar output for komorebi-bar, Waybar, and Polybar.
CONTROL

dpkg-deb --build "$DEB_STAGE" "$DIST/pycalendar_${VERSION}_amd64.deb"
rm -rf "$DEB_STAGE"
echo "  -> dist/pycalendar_${VERSION}_amd64.deb"

echo "Done."
