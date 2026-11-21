# iPhone and iPad

iOS doesn't let apps read the clipboard in the background, so no app can sync it automatically. Copia uses two Shortcuts instead, each assigned to tapping the back of the phone:

| Shortcut | Gesture | What it does |
|---|---|---|
| **Send to Copia** | Double tap | Sends what's on the clipboard (text, an image or a file) to the server |
| **Get from Copia** | Triple tap | Puts the server's current item on the clipboard |

While your Mac is awake and nearby, Apple's Universal Clipboard also carries what you copy on the iPhone to the Mac, and the [Mac agent](macos.md) forwards it automatically. The Shortcuts cover everything else.

## Install

You need a Mac with the agent configured (it provides the token and signs the shortcuts) and Tailscale, or another route to your server, on the iPhone.

1. On the Mac, in the repository:
   ```bash
   ./ios-shortcuts/build.sh            # optional argument: device name (default: iphone)
   ```
   This reads `COPIA_SERVER` and `COPIA_TOKEN` from `~/.config/copia/copia.env`, fills them into the templates, and signs the result with the built-in `shortcuts sign` (iOS only imports signed shortcuts). Two files appear on the Desktop.
2. AirDrop both files to the iPhone and tap **Add Shortcut** for each.
3. **Delete both files from the Mac's Desktop.** They contain your token.
4. On the iPhone:
   - **Settings → Accessibility → Touch → Back Tap:** Double Tap → *Send to Copia*, Triple Tap → *Get from Copia*
   - **Settings → Apps → Shortcuts → Paste from Other Apps → Allow**, so iOS doesn't ask "Allow Paste?" every time
   - Keep Tailscale connected

The templates in the repository (`ios-shortcuts/*.plist`) contain only placeholders (`__SERVER__`, `__TOKEN__`, `__DEVICE__`); your values exist only in the files `build.sh` writes to your Desktop.

iPad: same steps. If iCloud sync for Shortcuts is on, they appear on the iPad automatically.

## Using it

- **iPhone → other devices:** copy something, double-tap the back of the phone. A notification confirms *Sent to Copia*; your desktops have it on their clipboard a moment later.
- **Other devices → iPhone:** copy on the desktop, triple-tap the back of the phone, paste. Files land on the clipboard too; paste them in the Files app.

## How the shortcuts work

**Send to Copia** gets the clipboard and checks its type. Text is sent as a form field named `text`; anything else (photos, files) is sent as a file upload named `file`. Both go to `POST /clip` with your token and device name.

**Get from Copia** downloads `GET /clip/latest/raw` (the current item's content: text, an image, a file, or a zip of several files) and copies it to the clipboard.

You can open either shortcut in the Shortcuts app to see or adjust every step.
