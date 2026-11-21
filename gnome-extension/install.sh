#!/usr/bin/env bash
# Link the extension into ~/.local/share/gnome-shell/extensions so edits in this repo apply on next login.
set -euo pipefail

UUID="copia@grandmastr.github.io"
SRC="$(cd "$(dirname "$0")" && pwd)/$UUID"
DEST="$HOME/.local/share/gnome-shell/extensions/$UUID"

mkdir -p "$(dirname "$DEST")"
if [ -e "$DEST" ] && [ ! -L "$DEST" ]; then
  echo "error: $DEST exists and is not a symlink; remove it first" >&2
  exit 1
fi
ln -sfn "$SRC" "$DEST"
echo "Linked $DEST -> $SRC"

CONF="${XDG_CONFIG_HOME:-$HOME/.config}/copia/copia.env"
for key in COPIA_SERVER COPIA_TOKEN; do
  if ! grep -q "^$key=." "$CONF" 2>/dev/null; then
    echo "warning: no $key in $CONF (see copia.env.example); the extension needs it" >&2
  fi
done

if gnome-extensions info "$UUID" >/dev/null 2>&1; then
  gnome-extensions enable "$UUID"
  echo "Enabled. On Wayland, log out and back in to load code changes."
else
  echo "GNOME Shell hasn't seen the extension yet. On Wayland: log out and back in, then run:"
  echo "  gnome-extensions enable $UUID"
fi
