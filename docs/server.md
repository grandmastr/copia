# Server

The Copia server is a single Go binary with no third-party dependencies. It holds the current clipboard item on disk, accepts uploads from your devices, and pushes new items to connected desktops over [Server-Sent Events](api.md#live-events).

## Requirements

- An always-on Linux machine your devices can reach (a home server, a VM, a Raspberry Pi)
- Go 1.22 or newer on that machine (the deploy script builds there)
- systemd (the server runs as a *user* service, no root needed)
- SSH access to it from the computer you deploy from

[Tailscale](https://tailscale.com) is the easiest way to make the server reachable from every device, at home and away, without opening anything to the internet.

## Deploy

On the server, once, allow your user's services to run at boot without you being logged in:

```bash
sudo loginctl enable-linger "$USER"
```

From your workstation, in the repository:

```bash
./server/deploy/deploy.sh <ssh-host>
```

The script:

1. Copies the source to `~/copia/src` on the host.
2. On first run only, creates `~/copia/copia.env` with a random 64-character token (`openssl rand -hex 32`), readable only by you.
3. Builds `~/copia/bin/copia-server` on the host.
4. Installs and (re)starts the `copia.service` systemd user service.
5. Checks `http://127.0.0.1:8787/healthz` and prints `healthz: ok`.

Run the same command again to upgrade. Your token and data are kept.

### Getting the token to your devices

Each client needs the token from `~/copia/copia.env` on the server. Copy it over SSH rather than pasting it into chats or notes:

```bash
mkdir -p ~/.config/copia
scp <ssh-host>:copia/copia.env /tmp/copia-server.env
grep '^COPIA_TOKEN=' /tmp/copia-server.env >> ~/.config/copia/copia.env && rm /tmp/copia-server.env
chmod 600 ~/.config/copia/copia.env
```

Then add `COPIA_SERVER=http://<server>:8787` to the same file (see [`copia.env.example`](../copia.env.example)).

## Configuration

The server reads environment variables from `~/copia/copia.env` (via the systemd unit's `EnvironmentFile`). Restart after changing it: `systemctl --user restart copia`.

| Variable | Default | Meaning |
|---|---|---|
| `COPIA_TOKEN` | *(required)* | Shared secret, at least 16 characters |
| `COPIA_ADDR` | `:8787` | Address and port to listen on |
| `COPIA_DATA` | `~/copia/data` | Storage directory |
| `COPIA_MAX_ITEMS` | `1` | Items to keep. With 1, each copy replaces the last and the old one is deleted from disk |
| `COPIA_MAX_AGE` | `24h` | Older items are dropped after this, except the newest, which is kept until something replaces it |
| `COPIA_AUTOSYNC_LIMIT` | `20MB` | Larger items are marked `autoSync: false`; clients fetch them only when asked |
| `COPIA_MAX_UPLOAD` | `100MB` | Hard limit per item; larger uploads get `413` |

Sizes accept `B`, `KB`, `MB` and `GB` (binary multiples). Durations use Go syntax: `90m`, `24h`, `168h`.

## Operating it

| Task | Command (on the server) |
|---|---|
| Status | `systemctl --user status copia` |
| Live logs | `journalctl --user -u copia -f` |
| Restart | `systemctl --user restart copia` |
| Stop / start | `systemctl --user stop copia` / `start copia` |
| Health check | `curl http://127.0.0.1:8787/healthz` |
| Clear the clipboard | `rm -rf ~/copia/data/items/*` then restart |

Every request (except `/healthz`) is logged with method, path, status, size, duration, remote address and device name. Clipboard contents are never logged.

### Storage layout

```
~/copia/
├── copia.env            token and settings (mode 600)
├── bin/copia-server
├── src/                 source copied by deploy.sh
└── data/
    ├── items/<id>/      one folder per item
    │   ├── meta.json    kind, device, size, hash, file names
    │   └── part-0 …     the content itself
    └── tmp/             uploads in progress (cleared on start)
```

Uploads are written to `tmp/` and moved into place only when complete, so a half-finished upload never becomes the current item.

## Network exposure

The server listens on all interfaces by default. How far it is reachable depends on your firewall:

- **Tailscale only (recommended):** allow the port on the `tailscale0` interface and keep it closed elsewhere. On Fedora with firewalld, `sudo firewall-cmd --permanent --zone=trusted --add-interface=tailscale0 && sudo firewall-cmd --reload` trusts your tailnet; check that your LAN zone doesn't also open the port (Fedora Workstation's default zone allows ports 1025–65535 from the LAN).
- **Bind to one address:** set `COPIA_ADDR=<tailscale-ip>:8787` to listen only there. Note that the service will fail to start if Tailscale isn't up yet when it starts; it retries automatically (`Restart=on-failure`).

Never expose Copia directly to the internet. It uses plain HTTP and is designed for a private network. See [security](security.md).
