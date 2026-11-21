# Troubleshooting

Start with the logs:

| Where | Command |
|---|---|
| Server | `journalctl --user -u copia -f` (on the server) |
| Linux | `journalctl --user -f -o cat /usr/bin/gnome-shell \| grep copia` |
| macOS | `tail -f ~/Library/Logs/copia.log` |

And check the server is reachable from the device that isn't working:

```bash
curl http://<server>:8787/healthz                                  # expect: ok
curl -H "Authorization: Bearer <token>" http://<server>:8787/clip/latest
```

## Nothing syncs on one device

- **Tailscale is off** on that device (or on the server). The server name only resolves while Tailscale is connected. Turn on *Launch at login* in Tailscale's settings so it survives reboots. Clients reconnect on their own once it's back.
- **Wrong or missing settings.** `~/.config/copia/copia.env` needs both `COPIA_SERVER` and `COPIA_TOKEN`, each on its own line. A `401` in the logs means the token doesn't match the server's.
- **macOS: clipboard permission.** If macOS asked whether `copia-agent` may paste from other apps and you declined, allow it in System Settings → Privacy & Security.

## Linux

- **No "Copia" in the top bar** after installing: log out and back in. On Wayland, GNOME only loads new or updated extensions at login. Then check `gnome-extensions info copia@grandmastr.github.io` says `State: ACTIVE`.
- **"Not set up"** in the menu: the config file is missing `COPIA_SERVER` or `COPIA_TOKEN`.
- **A change to the extension had no effect:** log out and back in.
- **A large item didn't arrive:** items over 20 MB aren't synced automatically; open the Copia menu and click it.

## macOS

- **Agent not running:** `launchctl print gui/$(id -u)/io.github.grandmastr.copia | grep 'state ='`. Start it with `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.grandmastr.copia.plist`.
- **Build errors in `install.sh`:** make sure the command line tools are installed (`xcode-select --install`) and macOS is 12 or later.
- **Log says `not set up`:** add `COPIA_SERVER` and `COPIA_TOKEN` to `~/.config/copia/copia.env`; launchd restarts the agent automatically.

## iPhone / iPad

- **iOS asks "Allow Paste?" every time:** Settings → Apps → Shortcuts → Paste from Other Apps → Allow.
- **The shortcut fails with a connection error:** Tailscale isn't connected on the iPhone.
- **`build.sh` fails to sign:** `shortcuts sign` needs macOS 12+ and an Apple ID signed in on the Mac.
- **Something looks wrong inside a shortcut:** open it in the Shortcuts app; every step is visible and editable.

## Files

- **A folder didn't sync:** folders aren't supported yet; copy the files inside it.
- **"Not sent: … over the server's limit":** items are capped at 100 MB (`COPIA_MAX_UPLOAD` on the server).
