#!/usr/bin/env bash
# Copy the server source to the host, build it there, and (re)start the user service.
# Usage: ./deploy/deploy.sh [ssh-host]   (default: asgard)
set -euo pipefail

HOST="${1:-asgard}"
cd "$(dirname "$0")/.."

echo "==> Copying source to $HOST"
ssh "$HOST" 'mkdir -p ~/copia/src ~/copia/bin ~/copia/data ~/.config/systemd/user'
scp -q go.mod ./*.go ../README.md "$HOST":copia/src/
scp -q deploy/copia.service "$HOST":.config/systemd/user/copia.service

echo "==> Building and restarting on $HOST"
ssh "$HOST" 'bash -s' <<'REMOTE'
set -euo pipefail
if [ ! -f ~/copia/copia.env ]; then
  ( umask 077; printf 'COPIA_TOKEN=%s\n' "$(openssl rand -hex 32)" > ~/copia/copia.env )
  echo "    created ~/copia/copia.env with a new random token"
fi
cd ~/copia/src
go build -trimpath -ldflags='-s -w' -o ~/copia/bin/copia-server.new .
mv ~/copia/bin/copia-server.new ~/copia/bin/copia-server
systemctl --user daemon-reload
systemctl --user enable copia.service >/dev/null 2>&1
systemctl --user restart copia.service
sleep 1
systemctl --user --no-pager --lines=5 status copia.service || true
curl -fsS http://127.0.0.1:8787/healthz >/dev/null && echo "==> healthz: ok"
REMOTE
