#!/bin/bash
# Removes Outline Control from this Mac, in whichever mode it was installed.
# Saved exits and their keys live in the support directory; this script deletes
# them, so it asks first.
set -euo pipefail

SUPPORT="$HOME/Library/Application Support/Outline Control"
AGENT_PLIST="$HOME/Library/LaunchAgents/sh.cjt.outline-control.plist"
DAEMON_PLIST="/Library/LaunchDaemons/sh.cjt.outline-control.plist"
DAEMON_BIN="/Library/Application Support/Outline Control"
LABEL="sh.cjt.outline-control"

echo "This removes:"
echo "  /Applications/Outline Control.app"
echo "  $AGENT_PLIST  (socks mode)"
echo "  $DAEMON_PLIST + $DAEMON_BIN  (tunnel mode, needs sudo)"
echo "  $SUPPORT  (saved exits and their access keys)"
echo "  the Mac Remote module registration (modules/outline-control.json)"
printf 'Continue? [y/N] '
read -r answer
case "$answer" in
  y|Y) ;;
  *) echo "cancelled"; exit 0 ;;
esac

# Tunnel mode: a root daemon. Ask for sudo once, and only if it is installed.
if [ -f "$DAEMON_PLIST" ]; then
  sudo launchctl bootout "system/$LABEL" 2>/dev/null || true
  sudo rm -f "$DAEMON_PLIST"
  sudo rm -rf "$DAEMON_BIN"
fi

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
pkill -f "Contents/MacOS/OutlineControl" 2>/dev/null || true
rm -f "$AGENT_PLIST"
rm -rf "/Applications/Outline Control.app"
rm -rf "$SUPPORT"
rm -f "$HOME/Library/Application Support/MacOSRemoteControl/modules/outline-control.json"
echo "removed."
