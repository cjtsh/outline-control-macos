#!/bin/bash
# Installs Outline Control on this Mac:
#   * the GUI app into /Applications
#   * the background provider into ~/Library/Application Support/Outline Control
#   * a LaunchAgent that keeps the provider running and restarts it on crash
#
# Usage: ./install/install.sh [path/to/Outline Control.app]
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
APP_SRC="${1:-$ROOT/app/build/Outline Control.app}"
SUPPORT="$HOME/Library/Application Support/Outline Control"
PLIST="$HOME/Library/LaunchAgents/sh.cjt.outline-control.plist"
LABEL="sh.cjt.outline-control"

if [ ! -d "$APP_SRC" ]; then
  echo "No app bundle at: $APP_SRC"
  echo "Build it first:  ./app/build.sh"
  exit 1
fi

if ! command -v sslocal >/dev/null 2>&1 && [ ! -x /opt/homebrew/bin/sslocal ] && [ ! -x /usr/local/bin/sslocal ]; then
  cat <<'EOF'
A shadowsocks client is required. This project deliberately does not implement
the cipher itself; it supervises a proven one:

    brew install shadowsocks-rust

Then run this installer again.
EOF
  exit 1
fi

echo "==> installing provider to $SUPPORT"
mkdir -p "$SUPPORT"
chmod 700 "$SUPPORT"
cp "$APP_SRC/Contents/MacOS/outline-controld" "$SUPPORT/outline-controld"
chmod 755 "$SUPPORT/outline-controld"

echo "==> installing app to /Applications"
rm -rf "/Applications/Outline Control.app"
cp -R "$APP_SRC" "/Applications/Outline Control.app"

echo "==> writing LaunchAgent $PLIST"
mkdir -p "$HOME/Library/LaunchAgents"
cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$SUPPORT/outline-controld</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
	<key>StandardOutPath</key>
	<string>$SUPPORT/provider.log</string>
	<key>StandardErrorPath</key>
	<string>$SUPPORT/provider.log</string>
</dict>
</plist>
EOF

echo "==> starting the provider"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
# bootstrap only registers the job; kickstart actually runs it.
launchctl kickstart "gui/$(id -u)/$LABEL"

sleep 1
if curl -s -m 2 -H "X-Outline-Token: $(cat "$SUPPORT/token" 2>/dev/null || true)" \
     http://127.0.0.1:9911/servers >/dev/null 2>&1; then
  echo "==> provider is answering on 127.0.0.1:9911"
else
  echo "!! provider did not answer yet — check $SUPPORT/provider.log"
fi

echo
echo "Installed. Open \"/Applications/Outline Control.app\" to add an exit."
echo "The provider starts at login and is restartable with:"
echo "  launchctl kickstart gui/$(id -u)/$LABEL"
