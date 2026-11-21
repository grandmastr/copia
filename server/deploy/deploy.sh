#!/usr/bin/env bash
# Copy the server source to the host, build it there, and (re)start the user service.
# Usage: ./server/deploy/deploy.sh <ssh-host> [install-dir]
#   install-dir: absolute path on the host (default: ~/copia). It holds the binary,
#   source, token file (copia.env) and data. Use the same value on every deploy.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: $0 <ssh-host> [install-dir]   (default install-dir: ~/copia)" >&2
  exit 1
fi
HOST="$1"
DIR="${2:-}"
case "$DIR" in
  ""|/*) ;;
  *) echo "error: install-dir must be an absolute path (or omitted for ~/copia)" >&2; exit 1 ;;
esac
case "$DIR" in
  *[!A-Za-z0-9._/-]*) echo "error: install-dir may only contain letters, digits, . _ - /" >&2; exit 1 ;;
esac
cd "$(dirname "$0")/.."

# Resolve the default on the host itself, since its home directory may differ.
if [ -z "$DIR" ]; then
  DIR="$(ssh "$HOST" 'printf %s "$HOME/copia"')"
fi
echo "==> Installing to $HOST:$DIR"

ssh "$HOST" "mkdir -p '$DIR/src' '$DIR/bin' '$DIR/data' ~/.config/systemd/user"
scp -q go.mod ./*.go ../README.md "$HOST:$DIR/src/"
sed "s|__COPIA_DIR__|$DIR|g" deploy/copia.service | ssh "$HOST" 'cat > ~/.config/systemd/user/copia.service'

echo "==> Building and restarting on $HOST"
ssh "$HOST" "COPIA_DIR='$DIR' bash -s" <<'REMOTE'
set -euo pipefail
if [ ! -f "$COPIA_DIR/copia.env" ]; then
  ( umask 077; printf 'COPIA_TOKEN=%s\n' "$(openssl rand -hex 32)" > "$COPIA_DIR/copia.env" )
  echo "    created $COPIA_DIR/copia.env with a new random token"
fi
cd "$COPIA_DIR/src"
go build -trimpath -ldflags='-s -w' -o "$COPIA_DIR/bin/copia-server.new" .
mv "$COPIA_DIR/bin/copia-server.new" "$COPIA_DIR/bin/copia-server"
systemctl --user daemon-reload
systemctl --user enable copia.service >/dev/null 2>&1
systemctl --user restart copia.service
sleep 1
systemctl --user --no-pager --lines=5 status copia.service || true
curl -fsS http://127.0.0.1:8787/healthz >/dev/null && echo "==> healthz: ok"
REMOTE
