# iPhone and iPad

iOS doesn't let apps read or change the clipboard in the background: there is no setting, permission or app that allows it, and only Apple's own Universal Clipboard syncs automatically. So on iPhone and iPad, Copia uses two Shortcuts that you trigger:

| Shortcut | What it does |
|---|---|
| **Get from Copia** | Puts the server's current item (text, an image or a file) on the clipboard, ready to paste |
| **Send to Copia** | Sends what's on the clipboard to the server, so your other devices get it |

You choose how to trigger them: a tap on the back of the phone, the Action Button, a Control Center button, or automatically when you open certain apps. See [Triggers](#triggers).

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
   - **Settings → Apps → Shortcuts → Paste from Other Apps → Allow.** Required: *Send to Copia* reads the clipboard, and when it runs from a gesture iOS can't show the "Allow Paste?" prompt, so with *Ask* it silently gets nothing.
   - Set up your [triggers](#triggers).
   - Keep Tailscale connected.

The templates in the repository (`ios-shortcuts/*.plist`) contain only placeholders (`__SERVER__`, `__TOKEN__`, `__DEVICE__`); your values exist only in the files `build.sh` writes to your Desktop.

iPad: same steps. If iCloud sync for Shortcuts is on, the shortcuts appear on the iPad automatically.

## Triggers

Each gesture or button runs exactly one shortcut. Mix and match:

| Trigger | Where to set it | Notes |
|---|---|---|
| **Back Tap: double tap** | Settings → Accessibility → Touch → Back Tap → Double Tap | The most reliable gesture. Recommended for **Get from Copia** |
| **Back Tap: triple tap** | Same place → Triple Tap | Needs three quick, firm taps near the Apple logo; thick cases make it less reliable |
| **Action Button** (iPhone 15 Pro and later) | Settings → Action Button → Shortcut | One press, very reliable. A good home for **Send to Copia** |
| **Control Center** (iOS 18+) | Open Control Center → **+** → Add a Control → Shortcut | One tap from anywhere |
| **Home Screen widget** | Long-press the Home Screen → **+** → Shortcuts | Shows both shortcuts as buttons |
| **Automatically on app open** | See [Pasting on the iPhone](#pasting-on-the-iphone) | For *Get* only |

A good setup: **double tap → Get from Copia**, and **Send to Copia** on the Action Button, triple tap or Control Center.

### One button for both

If you only have one button free, make a small third shortcut that asks which way to go:

1. In Shortcuts, tap **+** and name it **Copia**.
2. Add **Choose from Menu** and rename its two items to **Send** and **Get**.
3. Under **Send**, add **Run Shortcut** → *Send to Copia*.
4. Under **Get**, add **Run Shortcut** → *Get from Copia*.
5. Put **Copia** on the button.

Pressing the button then shows a two-item menu. A single press can't safely work out the direction by itself: iOS doesn't tell a shortcut *when* something was copied, so it can't tell whether your iPhone clipboard is new (send it) or old (a copy from another device should replace it). Guessing wrong would overwrite your latest copy on the server.

## Sending from the iPhone

Copy something, then run **Send to Copia** with your trigger. A notification confirms *Sent to Copia*; your desktops have it on their clipboard a moment later.

**Automatic, through a Mac.** While your Mac is awake and nearby, anything you copy on the iPhone appears on the Mac within seconds through Universal Clipboard, and the [Mac agent](macos.md) forwards it to Copia, with no trigger at all. This needs Handoff on both devices (iPhone: Settings → General → AirPlay & Continuity → Handoff; Mac: System Settings → General → AirDrop & Handoff), the same Apple ID, and Wi-Fi and Bluetooth on. It stops when the Mac sleeps or is out of range, so keep a trigger for *Send to Copia* as well.

**Avoid sending on app close.** An automation that runs *Send to Copia* whenever you leave an app is possible, but it sends whatever is on the iPhone's clipboard even if you copied nothing new, and can overwrite something you just copied on another device. It isn't recommended.

## Pasting on the iPhone

Things you copy on other devices don't appear on the iPhone's clipboard by themselves. Something has to run **Get from Copia** before you paste. There are two good ways.

### Option 1: a gesture (works everywhere)

Put **Get from Copia** on Back Tap's double tap (or another [trigger](#triggers)). Then, in any app: double-tap the back of the phone, wait for *Copied from Copia*, and paste. Files land on the clipboard too; paste them in the Files app.

### Option 2: fetch automatically when you open an app

A personal automation can run **Get from Copia** every time you open the apps you usually paste into, so the latest copy is already waiting:

1. Open **Shortcuts → Automation → + → App**.
2. Choose the apps (for example Messages, Notes, WhatsApp, Mail).
3. Select **Is Opened**, and set it to **Run Immediately** (not *Run After Confirmation*).
4. Tap **Next** and choose **Get from Copia**.

Opening one of those apps now fetches the current item and puts it on the clipboard without a tap.

Keep in mind that this **replaces whatever is on the iPhone's clipboard** every time you open one of those apps, including something you just copied on the iPhone itself. Choose apps where you paste in from other devices more than you copy within the phone, and use the gesture everywhere else. The two options work well together.

## How the shortcuts work

**Send to Copia** gets the clipboard and checks its type. Text is sent as a form field named `text`; anything else (photos, files) is sent as a file upload named `file`. Both go to `POST /clip` with your token and device name.

**Get from Copia** first asks the server what the current item is (`GET /clip/latest`). For text, it downloads the content and converts it with *Get Text from Input* before copying, because Shortcuts can otherwise treat a downloaded text as a file, which pastes as nothing in a text field. Images and files (or a zip of several files) are copied as they are.

The templates are generated by `ios-shortcuts/generate.py` (Python standard library only); edit that script rather than the `.plist` files, then run it to regenerate them.

You can open either shortcut in the Shortcuts app to see or adjust every step.
