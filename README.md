# Copia

A self-hosted clipboard that syncs text, images and files between Linux, macOS, iPhone and iPad. Copy on one device and paste on another.

*Copia* is Latin for "abundance" or "supply", and is where the English word *copy* comes from.

## How it works

```
 Ubuntu (GNOME ext.) ─┐                       ┌─ live updates (SSE)
 macOS (Swift agent) ─┼── HTTP over Tailscale ─┤  Copia server (Go)
 iPhone / iPad ───────┘    (POST /clip)        └─ items on disk, pruned automatically
   (Shortcuts)
```

- **Server** (`server/`): a single Go binary with no third-party dependencies. It stores the latest clipboard items (text, an image, or one or more files) and pushes each new item to connected devices over Server-Sent Events.
- **Desktop agents** watch the local clipboard, upload new items, and apply items arriving from other devices. Items larger than the auto-sync limit only download when you paste.
- **iOS** (planned): two Shortcuts, *Send Clip* and *Get Clip*. iOS doesn't let apps watch the clipboard in the background, so on iPhone and iPad it's one tap rather than automatic.

Run it on a private network such as [Tailscale](https://tailscale.com) and protect it with a token. It isn't designed to be exposed to the public internet.

## Server

### Configuration (environment variables)

| Variable | Default | Meaning |
|---|---|---|
| `COPIA_TOKEN` | *(required)* | Shared secret, at least 16 characters. Generate one with `openssl rand -hex 32` |
| `COPIA_ADDR` | `:8787` | Address to listen on |
| `COPIA_DATA` | `~/copia/data` | Storage directory |
| `COPIA_MAX_ITEMS` | `1` | Items to keep. With 1 (the default), each copy replaces the last and the old one is deleted from disk |
| `COPIA_MAX_AGE` | `24h` | Drop older items after this. The newest item is always kept until something replaces it |
| `COPIA_AUTOSYNC_LIMIT` | `20MB` | Larger items are marked `autoSync: false`, so clients fetch them only on paste |
| `COPIA_MAX_UPLOAD` | `100MB` | Hard limit per item. Larger uploads are rejected with `413` |

### API

Every endpoint except `/healthz` needs `Authorization: Bearer <token>` (or `X-Copia-Token: <token>`).

| Method & path | Description |
|---|---|
| `POST /clip` | Upload an item (see below). Returns `201` with the item, or `200` + `X-Copia-Duplicate: 1` if it matches the current item |
| `GET /clip/latest` | Metadata of the newest item |
| `GET /clip/latest/raw` | Content of the newest item: the text, image or file itself, or a zip if it has several files |
| `GET /clip?limit=20` | Recent items, newest first |
| `GET /clip/{id}` · `/clip/{id}/raw` · `/clip/{id}/parts/{n}` | One item, its content, or one of its files |
| `DELETE /clip/{id}` | Remove an item |
| `GET /events` | Server-Sent Events: `hello` (on connect, includes the latest item and the size limits), `clip` (new item), `delete` |
| `GET /healthz` | Liveness check (no auth) |

You can upload in three ways:

```bash
# Raw body: set Content-Type, plus X-Copia-Filename for files
curl -H "Authorization: Bearer $T" -H "Content-Type: text/plain" --data-binary 'hello' http://host:8787/clip
curl -H "Authorization: Bearer $T" -H "Content-Type: image/png" -H "X-Copia-Filename: shot.png" \
     --data-binary @shot.png http://host:8787/clip

# Multipart form (what iOS Shortcuts sends): a "text" field and/or file fields
curl -H "Authorization: Bearer $T" -F device=iphone -F file=@report.pdf http://host:8787/clip

# URL-encoded form
curl -H "Authorization: Bearer $T" -d 'text=hello&device=mac' http://host:8787/clip
```

Optional: the `X-Copia-Device` header (or `device` field) records which device sent the item. `X-Copia-Kind: file` (or a `kind` field) makes a single image upload paste as a file rather than as image data.

Each item gets a content hash. Uploading the same content as the current item doesn't create a new one, which stops agents from re-uploading items they just received.

### Deploy (systemd user service)

On the server, once:

```bash
sudo loginctl enable-linger "$USER"   # start the service at boot without logging in
```

From your workstation:

```bash
./server/deploy/deploy.sh <ssh-host>
```

This copies the source to the host and builds it there (Go 1.22+ needed on the host). It creates `~/copia/copia.env` with a random token on first run, then installs and restarts the `copia.service` user service. Logs: `journalctl --user -u copia -f`.

## GNOME extension (`gnome-extension/`)

Watches the clipboard and uploads what you copy: text, images, and files copied in Nautilus. It also receives items from your other devices live. Received files are saved to `~/.cache/copia/<id>/` and pasted as normal files. A top-bar menu shows connection status, a sync on/off switch, "Send clipboard now", and recent items (click one to put it on this computer's clipboard, including large items that weren't synced automatically).

Received files are kept only while they're the current item: older downloads are deleted when a new item arrives, when the server removes an item, and at login. It skips anything a password manager marks as secret (`x-kde-passwordManagerHint`). Folders aren't synced yet.

Setup:

```bash
mkdir -p ~/.config/copia
# COPIA_TOKEN is required. COPIA_SERVER defaults to http://asgard:8787, COPIA_DEVICE to the hostname.
printf 'COPIA_TOKEN=%s\nCOPIA_SERVER=http://your-server:8787\n' "<token>" > ~/.config/copia/copia.env
chmod 600 ~/.config/copia/copia.env
./gnome-extension/install.sh      # on Wayland, log out and back in, then:
gnome-extensions enable copia@grandmastr.github.io
```

Logs: `journalctl --user -f -o cat /usr/bin/gnome-shell | grep copia`

## Status

- [x] Server
- [x] GNOME Shell extension (Ubuntu / Wayland)
- [ ] macOS agent (Swift)
- [ ] iOS Shortcuts
