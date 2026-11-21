#!/usr/bin/env bash
# Build the Copia agent and run it in the background at login (launchd user agent).
# Run on the Mac: ./mac-agent/install.sh
set -euo pipefail

LABEL="io.github.grandmastr.copia"
SRC="$(cd "$(dirname "$0")" && pwd)/copia-agent.swift"
BIN="$HOME/.local/bin/copia-agent"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG="$HOME/Library/Logs/copia.log"
CONF="$HOME/.config/copia/copia.env"

if ! command -v swiftc >/dev/null; then
  echo "swiftc not found. Install the command line tools first: xcode-select --install" >&2
  exit 1
fi
for key in COPIA_SERVER COPIA_TOKEN; do
  if ! grep -q "^$key=." "$CONF" 2>/dev/null; then
    echo "warning: no $key in $CONF (see copia.env.example); the agent will exit until you add it" >&2
  fi
done

mkdir -p "$(dirname "$BIN")" "$(dirname "$PLIST")" "$(dirname "$LOG")"
echo "==> Building"
swiftc -O -parse-as-library -o "$BIN.new" "$SRC"
mv "$BIN.new" "$BIN"

echo "==> Installing launch agent"
cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Interactive</string>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
  <key>StandardOutPath</key><string>$LOG</string>
  <key>StandardErrorPath</key><string>$LOG</string>
</dict>
</plist>
EOF

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
sleep 1
echo "==> Running. Last log lines ($LOG):"
tail -n 5 "$LOG" 2>/dev/null || true
echo
echo "Follow the log:   tail -f $LOG"
echo "Stop / uninstall: launchctl bootout gui/\$(id -u)/$LABEL && rm $PLIST $BIN"
