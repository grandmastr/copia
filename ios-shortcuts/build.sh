#!/usr/bin/env bash
# Run on a Mac. Fills in the server, token and device name from ~/.config/copia/copia.env,
# then signs the shortcuts so an iPhone/iPad can import them. Output goes to ~/Desktop.
# The signed files contain your token: delete them after importing.
set -euo pipefail

cd "$(dirname "$0")"
CONF="$HOME/.config/copia/copia.env"
DEVICE="${1:-iphone}"

TOKEN="$(sed -n 's/^COPIA_TOKEN=//p' "$CONF" | tr -d '"'"'"' ')"
SERVER="$(sed -n 's/^COPIA_SERVER=//p' "$CONF" | tr -d '"'"'"' ')"
SERVER="${SERVER%/}"

if [ -z "$TOKEN" ] || [ -z "$SERVER" ]; then
  echo "error: COPIA_SERVER and COPIA_TOKEN must be set in $CONF (see copia.env.example)" >&2
  exit 1
fi
if ! command -v shortcuts >/dev/null; then
  echo "error: the 'shortcuts' command needs macOS 12 or later" >&2
  exit 1
fi
case "$TOKEN$SERVER$DEVICE" in
  *'|'*|*'&'*|*'<'*|*'>'*) echo "error: unexpected characters in token, server or device name" >&2; exit 1 ;;
esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

for name in "Send to Copia" "Get from Copia" "Copia"; do
  sed -e "s|__TOKEN__|$TOKEN|g" -e "s|__SERVER__|$SERVER|g" -e "s|__DEVICE__|$DEVICE|g" \
    "$name.plist" > "$WORK/$name.plist"
  plutil -lint -s "$WORK/$name.plist"
  plutil -convert binary1 -o "$WORK/$name.unsigned.shortcut" "$WORK/$name.plist"
  shortcuts sign --mode anyone --input "$WORK/$name.unsigned.shortcut" --output "$HOME/Desktop/$name.shortcut"
  echo "signed: ~/Desktop/$name.shortcut"
done

echo
echo "AirDrop the files you want from your Desktop to the iPhone and tap 'Add Shortcut':"
echo "  Send to Copia + Get from Copia  (one trigger each), and/or"
echo "  Copia                           (one button with a Send / Get menu)"
echo "Then delete all three from the Desktop: they contain your Copia token."
