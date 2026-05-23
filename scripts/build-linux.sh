#!/usr/bin/env bash
# Build PyCalendar for Linux and create a tar.gz archive.
# Usage: ./scripts/build-linux.sh [VERSION]
set -euo pipefail

VERSION="${1:-0.1.0}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"

mkdir -p "$DIST"

echo "Building pycalendar v$VERSION for linux/amd64..."
GOARCH=amd64 GOOS=linux \
  go build -ldflags="-s -w -X main.version=$VERSION" \
  -o "$DIST/pycalendar" "$ROOT"
echo "  -> dist/pycalendar"

# ---- tar.gz archive (binary + desktop file + install script) ----
ARCHIVE_DIR="$DIST/pycalendar-$VERSION-linux-amd64"
mkdir -p "$ARCHIVE_DIR"
cp "$DIST/pycalendar"                             "$ARCHIVE_DIR/"
cp "$ROOT/installer/linux/pycalendar.desktop"     "$ARCHIVE_DIR/"
[[ -f "$ROOT/assets/icon.png" ]] && cp "$ROOT/assets/icon.png" "$ARCHIVE_DIR/"

cat > "$ARCHIVE_DIR/install.sh" <<'INSTALL'
#!/usr/bin/env bash
set -e
sudo install -Dm755 pycalendar /usr/local/bin/pycalendar
sudo install -Dm644 pycalendar.desktop /usr/share/applications/pycalendar.desktop
[[ -f icon.png ]] && sudo install -Dm644 icon.png /usr/share/pixmaps/pycalendar.png
command -v update-desktop-database &>/dev/null && sudo update-desktop-database /usr/share/applications || true
echo "Installed. Run: pycalendar --mode tray"
INSTALL
chmod +x "$ARCHIVE_DIR/install.sh"

tar -czf "$DIST/pycalendar-$VERSION-linux-amd64.tar.gz" -C "$DIST" "pycalendar-$VERSION-linux-amd64"
rm -rf "$ARCHIVE_DIR"
echo "  -> dist/pycalendar-$VERSION-linux-amd64.tar.gz"

echo "Done."
