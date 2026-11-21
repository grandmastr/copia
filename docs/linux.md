# Linux (GNOME)

On Linux, Copia is a GNOME Shell extension. Because it runs inside the desktop, GNOME tells it the moment the clipboard changes: there is no polling, and no separate program to keep running.

**Supported:** GNOME 48, 49 and 50 on Wayland (Ubuntu 25.04+, Fedora 42+, and others).

## Install

1. Create `~/.config/copia/copia.env` from [`copia.env.example`](../copia.env.example) with your `COPIA_SERVER` and `COPIA_TOKEN`, and `chmod 600` it.
2. Link the extension into place:
   ```bash
   ./gnome-extension/install.sh
   ```
3. Log out and back in. On Wayland, GNOME only loads new extensions (and new versions of them) at login.
4. Enable it, if the installer couldn't already:
   ```bash
   gnome-extensions enable copia@grandmastr.github.io
   ```

**Copia** appears in the top bar. The installer links the extension to your clone of the repository, so `git pull` plus a logout updates it.

## Using it

Copy as usual: text, screenshots, images from a browser, and files with Ctrl+C in the Files app. Copies of 20 MB or less go to the server immediately; things your other devices copy arrive on your clipboard the same way. Paste with Ctrl+V. Received files paste into the Files app as ordinary files.

The **Copia** menu in the top bar has:

| Item | What it does |
|---|---|
| Status line | `Connected to <server>`, or why it isn't (the label dims while disconnected) |
| **Sync this computer** | Pause and resume syncing in both directions |
| **Send clipboard now** | Send the current clipboard, including items over the auto-sync limit (up to the server's 100 MB maximum) |
| **On the server** | The current item; click it to put it on this computer's clipboard (useful for large items that weren't synced automatically) |

## What gets synced

| You copy | Sent as |
|---|---|
| Text | Text |
| An image (screenshot, "Copy Image") | Image (PNG) |
| One or more files in the Files app | Files, with their names |
| Text *and* an image together (e.g. spreadsheet cells) | Text |
| A folder | Not synced yet |
| Anything a password manager marks secret (`x-kde-passwordManagerHint`) | Never sent |

Received files are saved to `~/.cache/copia/<id>/` and put on the clipboard as file references. Only the current item's files are kept: older downloads are deleted when a new item arrives, when the server replaces an item, and at every login.

## Logs

```bash
journalctl --user -f -o cat /usr/bin/gnome-shell | grep copia
```

For more detail, add `COPIA_DEBUG=1` to `~/.config/copia/copia.env` and log out and back in: the extension will also log which clipboard formats each copy offered (formats only, never contents).

## Uninstall

```bash
gnome-extensions disable copia@grandmastr.github.io
rm ~/.local/share/gnome-shell/extensions/copia@grandmastr.github.io
rm -rf ~/.cache/copia ~/.config/copia
```
