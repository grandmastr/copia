# macOS

On macOS, Copia is a small background agent written in Swift, using only Apple's frameworks. launchd starts it at login and restarts it if it ever stops. There is no window and no Dock icon.

**Supported:** macOS 12 Monterey and later.

## Install

1. Install Apple's command line tools if you don't have Xcode: `xcode-select --install`
2. Create `~/.config/copia/copia.env` from [`copia.env.example`](../copia.env.example) with your `COPIA_SERVER` and `COPIA_TOKEN`, and `chmod 600` it.
3. Build and start the agent:
   ```bash
   ./mac-agent/install.sh
   ```

The script compiles `mac-agent/copia-agent.swift` to `~/.local/bin/copia-agent`, installs the launch agent `io.github.grandmastr.copia`, starts it, and shows the last lines of the log. You should see `copia agent started` and `connected to …`.

Newer macOS versions may ask once whether `copia-agent` may paste from other apps. Choose **Allow**, or the agent can't read what you copy.

To update after `git pull`, run `./mac-agent/install.sh` again.

## Using it

Copy and paste as usual with Cmd+C and Cmd+V. Files copied in Finder are sent with their names; files from your other devices paste into Finder as ordinary files.

Items over the auto-sync limit (20 MB by default) are skipped and noted in the log; fetch them with `curl` (see the [API](api.md)) or from another device.

## How it watches the clipboard

macOS has no clipboard-changed notification, so the agent reads the pasteboard's change counter twice a second. That is a single number kept by macOS: reading it costs effectively nothing, and the clipboard contents are read only when the number changes. Items from the server arrive over one long-lived connection, so there is no polling of the server.

| You copy | Sent as |
|---|---|
| Files in Finder | Files |
| Text | Text |
| An image | Image (PNG; TIFF is converted) |
| A folder | Not synced yet |
| Anything marked concealed or transient by a password manager ([nspasteboard.org](http://nspasteboard.org) types, 1Password) | Never sent |

Received files are saved to `~/Library/Caches/copia/<id>/`; only the current item's files are kept.

## Operating it

| Task | Command |
|---|---|
| Live log | `tail -f ~/Library/Logs/copia.log` |
| Is it running? | `launchctl print gui/$(id -u)/io.github.grandmastr.copia \| grep 'state ='` |
| Stop | `launchctl bootout gui/$(id -u)/io.github.grandmastr.copia` |
| Start | `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.grandmastr.copia.plist` |

If the server can't be reached (for example, Tailscale is off), the agent retries after 1, 2, 4, 8 and 16 seconds, then every 30 seconds, and reconnects on its own.

## Uninstall

```bash
launchctl bootout gui/$(id -u)/io.github.grandmastr.copia
rm ~/Library/LaunchAgents/io.github.grandmastr.copia.plist ~/.local/bin/copia-agent
rm -rf ~/Library/Caches/copia ~/Library/Logs/copia.log ~/.config/copia
```
