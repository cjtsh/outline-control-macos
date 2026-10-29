#!/bin/bash
# Removes Outline Control from this Mac. Saved exits and their keys live in the
# support directory; this script deletes them, so it asks first.
set -euo pipefail

SUPPORT="$HOME/Library/Application Support/Outline Control"
PLIST="$HOME/Library/LaunchAgents/sh.cjt.outline-control.plist"
LABEL="sh.cjt.outline-control"

echo "This removes:"
echo "  /Applications/Outline Control.app"
echo "  $PLIST"
echo "  $SUPPORT  (saved exits and their access keys)"
printf 'Continue? [y/N] '
read -r answer
case "$answer" in
  y|Y) ;;
  *) echo "cancelled"; exit 0 ;;
esac

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
pkill -f "Contents/MacOS/OutlineControl" 2>/dev/null || true
rm -f "$PLIST"
rm -rf "/Applications/Outline Control.app"
rm -rf "$SUPPORT"
echo "removed."
