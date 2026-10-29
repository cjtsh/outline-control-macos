#!/bin/bash
# Install Outline Control as a full-device VPN.
#
# Unlike install.sh (a per-user SOCKS proxy), this installs a root LaunchDaemon:
# taking the default route away from the Mac needs root, and only a daemon
# survives logout and reboot.
#
#   sudo ./install/install-tunnel.sh [--accept-terms] [--exclude-host NAME] [--exclude-cidr CIDR]
#
# Exclusions are the addresses that must keep working when the VPN is dead —
# in practice the relay the phone talks to. Pass them once, they are baked into
# the daemon's arguments.
set -euo pipefail

# --- acceptance (software-disclaimer standard, section 5.3) -----------------
NOTICE_URL="https://github.com/cjtsh/software-disclaimer/blob/main/DISCLAIMER.md"
print_notice() {
  cat <<'NOTICE'
Outline Control is free and open-source software, provided "AS IS" and without
warranties. It reconfigures this Mac's network, it needs administrator rights,
and you use it at your own risk. Keep backups. Do not use it in life-safety or
mission-critical settings. Bitseeker LLC and its contributors disclaim liability
as far as the law allows.

Full terms: https://github.com/cjtsh/software-disclaimer/blob/main/DISCLAIMER.md
NOTICE
}

# Fails closed: unattended runs must pass --accept-terms.
require_acceptance() {
  local accepted=0
  for arg in "$@"; do
    [ "$arg" = "--accept-terms" ] && accepted=1
  done
  print_notice
  if [ "$accepted" -eq 1 ]; then
    echo "==> terms accepted (--accept-terms)"
    return 0
  fi
  if [ ! -t 0 ]; then
    echo "" >&2
    echo "This installer needs you to accept the terms first:" >&2
    echo "    $0 --accept-terms $*" >&2
    exit 1
  fi
  printf "\nType ACCEPT to install, or anything else to stop: "
  read -r reply
  if [ "$reply" != "ACCEPT" ]; then
    echo "Not accepted; nothing was installed." >&2
    exit 1
  fi
}

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_DIR="${OUTLINE_CONTROL_DATA:-$HOME/Library/Application Support/Outline Control}"
LABEL="sh.cjt.outline-control"
PLIST="/Library/LaunchDaemons/$LABEL.plist"
BIN_DIR="/Library/Application Support/Outline Control"
EXCLUDES=()

ACCEPT_TERMS=0
ORIGINAL_ARGS=("$@")
while [ $# -gt 0 ]; do
  case "$1" in
    --exclude-host) EXCLUDES+=(-exclude-host "$2"); shift 2 ;;
    --exclude-cidr) EXCLUDES+=(-exclude-cidr "$2"); shift 2 ;;
    --accept-terms) ACCEPT_TERMS=1; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "This installer has to run as root: sudo $0 $*" >&2
  exit 1
fi

require_acceptance "${ORIGINAL_ARGS[@]}"

TARGET_USER="${SUDO_USER:-$(stat -f '%Su' /dev/console)}"
if [ -z "$TARGET_USER" ] || [ "$TARGET_USER" = "root" ]; then
  echo "cannot work out which desktop user should own the token" >&2
  exit 1
fi
DATA_DIR="${DATA_DIR/#\~/$HOME}"

SINGBOX="$(command -v sing-box || true)"
if [ -z "$SINGBOX" ]; then
  for candidate in /opt/homebrew/bin/sing-box /usr/local/bin/sing-box; do
    [ -x "$candidate" ] && SINGBOX="$candidate" && break
  done
fi
if [ -z "$SINGBOX" ]; then
  echo "sing-box is required for the full tunnel:  brew install sing-box" >&2
  exit 1
fi

echo "==> building the provider"
# Build as the desktop user: root has no Go caches on this machine.
sudo -u "$TARGET_USER" bash -c "cd '$REPO_DIR/provider' && go build -o '$REPO_DIR/app/build/outline-controld' ."

echo "==> installing the privileged daemon"
mkdir -p "$BIN_DIR"
install -m 0755 -o root -g wheel "$REPO_DIR/app/build/outline-controld" "$BIN_DIR/outline-controld"

# One owner of port 9911 only: the per-user agent has to go.
sudo -u "$TARGET_USER" launchctl bootout "gui/$(id -u "$TARGET_USER")/$LABEL" 2>/dev/null || true
rm -f "/Users/$TARGET_USER/Library/LaunchAgents/$LABEL.plist"

mkdir -p "$DATA_DIR"
chown "$TARGET_USER" "$DATA_DIR"
chmod 700 "$DATA_DIR"

echo "==> writing $PLIST"
{
  echo '<?xml version="1.0" encoding="UTF-8"?>'
  echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">'
  echo '<plist version="1.0"><dict>'
  echo "  <key>Label</key><string>$LABEL</string>"
  echo '  <key>ProgramArguments</key><array>'
  echo "    <string>$BIN_DIR/outline-controld</string>"
  echo '    <string>-mode</string><string>tunnel</string>'
  echo "    <string>-data</string><string>$DATA_DIR</string>"
  echo "    <string>-token-owner</string><string>$TARGET_USER</string>"
  for ((i = 0; i < ${#EXCLUDES[@]}; i += 2)); do
    echo "    <string>${EXCLUDES[i]}</string><string>${EXCLUDES[i + 1]}</string>"
  done
  echo '  </array>'
  echo '  <key>RunAtLoad</key><true/>'
  echo '  <key>KeepAlive</key><true/>'
  echo "  <key>StandardOutPath</key><string>$DATA_DIR/provider.log</string>"
  echo "  <key>StandardErrorPath</key><string>$DATA_DIR/provider.log</string>"
  echo '</dict></plist>'
} > "$PLIST"
chown root:wheel "$PLIST"
chmod 0644 "$PLIST"

echo "==> starting the daemon"
pkill -f "$BIN_DIR/outline-controld" 2>/dev/null || true
launchctl bootout "system/$LABEL" 2>/dev/null || true
# launchd can answer the first bootstrap with "5: Input/output error" while it
# is still tearing the old job down, so retry instead of failing the install.
for attempt in 1 2 3 4 5; do
  if launchctl bootstrap system "$PLIST" 2>/dev/null; then
    break
  fi
  if [ "$attempt" = 5 ]; then
    echo "!! could not register the daemon with launchd" >&2
    launchctl bootstrap system "$PLIST"
    exit 1
  fi
  sleep 1
done
launchctl kickstart -k "system/$LABEL"

for _ in $(seq 1 25); do
  if [ -f "$DATA_DIR/token" ]; then
    TOKEN="$(cat "$DATA_DIR/token")"
    if curl -fsS -m 2 -H "X-Outline-Token: $TOKEN" http://127.0.0.1:9911/servers >/dev/null 2>&1; then
      echo "==> the VPN daemon is answering on 127.0.0.1:9911"
      break
    fi
  fi
  sleep 0.4
done

echo
echo "Installed. Full-device VPN mode:"
echo "  app      /Applications/Outline Control.app"
echo "  daemon   $BIN_DIR/outline-controld  (root LaunchDaemon $LABEL)"
echo "  engine   $SINGBOX"
echo "  data     $DATA_DIR"
echo
echo "Excluded from the tunnel (these keep working with the VPN off):"
if [ ${#EXCLUDES[@]} -eq 0 ]; then
  echo "  (none — pass --exclude-host / --exclude-cidr to protect a relay)"
else
  for ((i = 0; i < ${#EXCLUDES[@]}; i += 2)); do
    echo "  ${EXCLUDES[i + 1]}"
  done
fi
