// Copia: GNOME Shell extension that syncs the clipboard with a Copia server.
//
// Config: ~/.config/copia/copia.env
//   COPIA_TOKEN=...                       (required)
//   COPIA_SERVER=http://asgard:8787       (optional)
//   COPIA_DEVICE=my-laptop                (optional, defaults to the hostname)

import Clutter from 'gi://Clutter';
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import GObject from 'gi://GObject';
import Meta from 'gi://Meta';
import Soup from 'gi://Soup?version=3.0';
import St from 'gi://St';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

Gio._promisify(Soup.Session.prototype, 'send_and_read_async');
Gio._promisify(Soup.Session.prototype, 'send_async');
Gio._promisify(Gio.File.prototype, 'load_contents_async');
Gio._promisify(Gio.File.prototype, 'query_info_async');

// These two pair an _async call with a differently named _finish. GNOME Shell may
// already have promisified them with other finish functions (Gio._promisify is a
// no-op the second time), so call them explicitly instead.
function readLine(stream, cancellable) {
    return new Promise((resolve, reject) => {
        stream.read_line_async(GLib.PRIORITY_DEFAULT, cancellable, (s, res) => {
            try {
                resolve(s.read_line_finish_utf8(res)[0]); // string, or null at end of stream
            } catch (e) {
                reject(e);
            }
        });
    });
}

function writeFile(file, bytes, cancellable) {
    return new Promise((resolve, reject) => {
        file.replace_contents_bytes_async(bytes, null, false, Gio.FileCreateFlags.REPLACE_DESTINATION, cancellable, (f, res) => {
            try {
                resolve(f.replace_contents_finish(res));
            } catch (e) {
                reject(e);
            }
        });
    });
}

const CLIPBOARD = St.ClipboardType.CLIPBOARD;
const DEFAULT_SERVER = 'http://asgard:8787';
const DEFAULT_AUTOSYNC_LIMIT = 20 * 1024 * 1024;
const DEFAULT_MAX_UPLOAD = 100 * 1024 * 1024;
const RECENT_LIMIT = 8;
// Clipboard owners that mark their content as secret (e.g. KeePassXC). Never upload these.
const SECRET_HINTS = ['x-kde-passwordManagerHint'];
const TEXT_TYPES = ['text/plain;charset=utf-8', 'UTF8_STRING', 'text/plain', 'STRING'];
const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif', 'image/bmp', 'image/tiff'];
// What Nautilus itself puts on the clipboard for copied files.
const FILES_TYPE = 'x-special/gnome-copied-files';

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function log(msg) {
    console.log(`[copia] ${msg}`);
}

function isCancelled(e) {
    return e instanceof GLib.Error && e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED);
}

function readConfig() {
    const path = GLib.build_filenamev([GLib.get_user_config_dir(), 'copia', 'copia.env']);
    const cfg = {};
    try {
        const [, contents] = GLib.file_get_contents(path);
        for (const raw of decoder.decode(contents).split('\n')) {
            const line = raw.trim();
            if (!line || line.startsWith('#') || !line.includes('='))
                continue;
            const i = line.indexOf('=');
            cfg[line.slice(0, i).trim()] = line.slice(i + 1).trim().replace(/^["']|["']$/g, '');
        }
    } catch (e) {
        log(`cannot read ${path}: ${e.message}`);
    }
    return {
        path,
        token: cfg.COPIA_TOKEN ?? '',
        server: (cfg.COPIA_SERVER || DEFAULT_SERVER).replace(/\/+$/, ''),
        device: cfg.COPIA_DEVICE || GLib.get_host_name(),
        debug: cfg.COPIA_DEBUG ?? '',
    };
}

function clipboardText() {
    return new Promise(resolve => {
        St.Clipboard.get_default().get_text(CLIPBOARD, (_c, text) => resolve(text));
    });
}

// Resolves to a Uint8Array copy (or null). The GBytes passed to the callback is only
// valid during the callback, so it must be copied before the promise resolves.
function clipboardContent(mimetype) {
    return new Promise(resolve => {
        St.Clipboard.get_default().get_content(CLIPBOARD, mimetype, (_c, bytes) => {
            resolve(bytes && bytes.get_size() > 0 ? new Uint8Array(bytes.toArray()) : null);
        });
    });
}

// Recursively delete a file or directory (small trees only: runs synchronously).
function deleteTree(file) {
    try {
        const children = file.enumerate_children('standard::name,standard::type', Gio.FileQueryInfoFlags.NOFOLLOW_SYMLINKS, null);
        let info;
        while ((info = children.next_file(null)) !== null) {
            const child = file.get_child(info.get_name());
            if (info.get_file_type() === Gio.FileType.DIRECTORY)
                deleteTree(child);
            else
                child.delete(null);
        }
        children.close(null);
    } catch (e) {
        if (!(e instanceof GLib.Error) ||
            !(e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND) || e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_DIRECTORY)))
            log(`cleanup ${file.get_path()}: ${e.message}`);
    }
    try {
        file.delete(null);
    } catch {}
}

function parseUriList(text, skipFirstLine) {
    let lines = text.split('\n').map(l => l.trim()).filter(l => l && !l.startsWith('#'));
    if (skipFirstLine)
        lines = lines.slice(1); // "copy" or "cut"
    return lines;
}

function oneLine(s, max) {
    s = (s ?? '').replace(/\s+/g, ' ').trim();
    return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

function describe(item) {
    let what;
    if (item.kind === 'text')
        what = `“${oneLine(item.preview, 40)}”`;
    else if (item.kind === 'image')
        what = `Image · ${GLib.format_size(item.size)}`;
    else if (item.parts.length === 1)
        what = `${oneLine(item.parts[0].name || 'File', 32)} · ${GLib.format_size(item.size)}`;
    else
        what = `${item.parts.length} files · ${GLib.format_size(item.size)}`;
    return item.device ? `${what}  — ${item.device}` : what;
}

const CopiaIndicator = GObject.registerClass(
class CopiaIndicator extends PanelMenu.Button {
    _init(copia) {
        super._init(0.0, 'Copia');
        this._copia = copia;

        this._label = new St.Label({text: 'Copia', y_align: Clutter.ActorAlign.CENTER});
        this.add_child(this._label);

        this._status = new PopupMenu.PopupMenuItem('Connecting…', {reactive: false});
        this.menu.addMenuItem(this._status);

        this._switch = new PopupMenu.PopupSwitchMenuItem('Sync this computer', true);
        this._switch.connect('toggled', (_item, state) => copia.setSyncEnabled(state));
        this.menu.addMenuItem(this._switch);

        const send = new PopupMenu.PopupMenuItem('Send clipboard now');
        send.connect('activate', () => copia.sendNow());
        this.menu.addMenuItem(send);

        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem('On the server — click to paste here'));
        this._recent = new PopupMenu.PopupMenuSection();
        this.menu.addMenuItem(this._recent);
        this.setRecent([]);
    }

    setStatus(text, connected) {
        this._status.label.text = text;
        this._label.opacity = connected ? 255 : 110;
    }

    setRecent(items) {
        this._recent.removeAll();
        if (!items.length) {
            this._recent.addMenuItem(new PopupMenu.PopupMenuItem('Empty', {reactive: false}));
            return;
        }
        for (const item of items) {
            const mi = new PopupMenu.PopupMenuItem(describe(item));
            mi.connect('activate', () => this._copia.pasteItem(item));
            this._recent.addMenuItem(mi);
        }
    }
});

class Copia {
    constructor(uuid) {
        this._uuid = uuid;
        this._cfg = readConfig();
        this._running = true;
        this._syncEnabled = true;
        this._autoSyncLimit = DEFAULT_AUTOSYNC_LIMIT;
        this._maxUpload = DEFAULT_MAX_UPLOAD;
        this._recent = [];
        this._seenIds = new Set();
        this._lastSig = null;     // signature of what we last uploaded or applied
        this._ignoreUntil = 0;    // ignore clipboard changes we cause ourselves
        this._debounceId = 0;
        this._sleepId = 0;
        this._connected = false;
        this._cancellable = new Gio.Cancellable();
        this._cacheDir = GLib.build_filenamev([GLib.get_user_cache_dir(), 'copia']);
        this._debug = ['1', 'true', 'yes'].includes((this._cfg.debug ?? '').toLowerCase());
        this._clearCache(null); // files received in a previous session are no longer on any clipboard

        this._session = new Soup.Session({timeout: 60, user_agent: 'copia-gnome/0.1'});
        this._eventsSession = new Soup.Session({timeout: 0, idle_timeout: 0, user_agent: 'copia-gnome/0.1'});

        this._indicator = new CopiaIndicator(this);
        Main.panel.addToStatusArea(uuid, this._indicator);

        if (!this._cfg.token) {
            this._indicator.setStatus(`No token — add COPIA_TOKEN to ${this._cfg.path}`, false);
            return;
        }

        this._selection = global.display.get_selection();
        this._ownerChangedId = this._selection.connect('owner-changed', (_sel, type) => {
            if (type === Meta.SelectionType.SELECTION_CLIPBOARD)
                this._onLocalChange();
        });

        this._eventLoop().catch(e => log(`event loop crashed: ${e.message}`));
    }

    destroy() {
        this._running = false;
        this._cancellable.cancel();
        this._eventsCancellable?.cancel();
        if (this._ownerChangedId)
            this._selection.disconnect(this._ownerChangedId);
        for (const id of [this._debounceId, this._sleepId]) {
            if (id)
                GLib.source_remove(id);
        }
        this._session.abort();
        this._eventsSession.abort();
        this._indicator.destroy();
        this._indicator = null;
    }

    // ---------- HTTP ----------

    _message(method, path) {
        const msg = Soup.Message.new(method, `${this._cfg.server}${path}`);
        if (!msg)
            throw new Error(`invalid server URL: ${this._cfg.server}`);
        msg.request_headers.append('Authorization', `Bearer ${this._cfg.token}`);
        msg.request_headers.append('X-Copia-Device', this._cfg.device);
        return msg;
    }

    async _send(msg) {
        const bytes = await this._session.send_and_read_async(msg, GLib.PRIORITY_DEFAULT, this._cancellable);
        const status = msg.get_status();
        if (status < 200 || status >= 300) {
            let detail = '';
            try {
                detail = JSON.parse(decoder.decode(bytes.toArray())).error;
            } catch {}
            throw new Error(`HTTP ${status}${detail ? `: ${detail}` : ''}`);
        }
        return bytes;
    }

    async _getJSON(path) {
        const bytes = await this._send(this._message('GET', path));
        return JSON.parse(decoder.decode(bytes.toArray()));
    }

    // ---------- live events from the server ----------

    async _eventLoop() {
        let backoff = 1;
        let firstConnect = true;
        while (this._running) {
            this._eventsCancellable = new Gio.Cancellable();
            try {
                const msg = this._message('GET', '/events');
                const stream = await this._eventsSession.send_async(msg, GLib.PRIORITY_DEFAULT, this._eventsCancellable);
                if (msg.get_status() !== 200)
                    throw new Error(`HTTP ${msg.get_status()}`);
                backoff = 1;
                const data = new Gio.DataInputStream({base_stream: stream, close_base_stream: true});
                let event = 'message';
                let lines = [];
                for (;;) {
                    // eslint-disable-next-line no-await-in-loop
                    const line = await readLine(data, this._eventsCancellable);
                    if (line === null)
                        break; // server closed the stream
                    if (line === '') {
                        if (lines.length)
                            this._onEvent(event, lines.join('\n'), firstConnect);
                        if (event === 'hello')
                            firstConnect = false;
                        event = 'message';
                        lines = [];
                    } else if (!line.startsWith(':')) {
                        const i = line.indexOf(':');
                        const field = i < 0 ? line : line.slice(0, i);
                        let value = i < 0 ? '' : line.slice(i + 1);
                        if (value.startsWith(' '))
                            value = value.slice(1);
                        if (field === 'event')
                            event = value;
                        else if (field === 'data')
                            lines.push(value);
                    }
                }
            } catch (e) {
                if (!this._running || isCancelled(e))
                    return;
                log(`events: ${e.message}`);
            }
            this._setConnected(false, `Not connected — retrying in ${backoff}s`);
            // eslint-disable-next-line no-await-in-loop
            await this._sleep(backoff);
            backoff = Math.min(backoff * 2, 30);
        }
    }

    _sleep(seconds) {
        return new Promise(resolve => {
            this._sleepId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, seconds, () => {
                this._sleepId = 0;
                resolve();
                return GLib.SOURCE_REMOVE;
            });
        });
    }

    _setConnected(connected, text) {
        this._connected = connected;
        this._indicator?.setStatus(text, connected);
    }

    _onEvent(event, data, firstConnect) {
        let payload;
        try {
            payload = JSON.parse(data);
        } catch {
            return;
        }
        switch (event) {
        case 'hello': {
            this._autoSyncLimit = payload.autoSyncLimit ?? DEFAULT_AUTOSYNC_LIMIT;
            this._maxUpload = payload.maxUpload ?? DEFAULT_MAX_UPLOAD;
            const host = GLib.Uri.parse(this._cfg.server, GLib.UriFlags.NONE).get_host();
            this._setConnected(true, `Connected to ${host}`);
            this._refreshRecent();
            const latest = payload.latest;
            // On (re)connect, catch up on an item we missed while offline,
            // but never overwrite the clipboard on first start (e.g. at login).
            if (latest && !firstConnect && !this._seenIds.has(latest.id) && latest.device !== this._cfg.device)
                this._receive(latest);
            if (latest)
                this._seenIds.add(latest.id);
            break;
        }
        case 'clip':
            this._remember(payload);
            if (payload.device !== this._cfg.device)
                this._receive(payload);
            break;
        case 'delete':
            // Expired or replaced on the server: drop our downloaded copy too.
            if (/^[0-9a-f]{16}-[0-9a-f]{8}$/.test(payload.id ?? ''))
                deleteTree(Gio.File.new_for_path(GLib.build_filenamev([this._cacheDir, payload.id])));
            this._recent = this._recent.filter(i => i.id !== payload.id);
            this._indicator?.setRecent(this._recent);
            break;
        }
    }

    async _refreshRecent() {
        try {
            const {items} = await this._getJSON(`/clip?limit=${RECENT_LIMIT}`);
            this._recent = items;
            for (const i of items)
                this._seenIds.add(i.id);
            this._indicator?.setRecent(this._recent);
        } catch (e) {
            if (!isCancelled(e))
                log(`recent: ${e.message}`);
        }
    }

    _remember(item) {
        this._seenIds.add(item.id);
        this._recent = [item, ...this._recent.filter(i => i.id !== item.id)].slice(0, RECENT_LIMIT);
        this._indicator?.setRecent(this._recent);
    }

    _receive(item) {
        if (!this._syncEnabled)
            return;
        if (!item.autoSync) {
            Main.notify('Copia', `${describe(item)} is large, so it wasn't copied automatically. Click it in the Copia menu to paste it here.`);
            return;
        }
        this._apply(item).catch(e => {
            if (!isCancelled(e))
                log(`apply ${item.id}: ${e.message}`);
        });
    }

    // Menu click: put any item on this computer's clipboard, whatever its size.
    pasteItem(item) {
        this._apply(item).catch(e => {
            if (!isCancelled(e))
                Main.notify('Copia', `Couldn't fetch that item: ${e.message}`);
        });
    }

    // ---------- applying a remote item to the local clipboard ----------

    async _apply(item) {
        const clipboard = St.Clipboard.get_default();
        if (item.kind === 'text') {
            const bytes = await this._send(this._message('GET', `/clip/${item.id}/raw`));
            const text = decoder.decode(bytes.toArray());
            this._lastSig = `text:${text}`;
            this._ignoreUntil = Date.now() + 1000;
            clipboard.set_text(CLIPBOARD, text);
            this._clearCache(null);
            return;
        }
        if (item.kind === 'image') {
            const bytes = await this._send(this._message('GET', `/clip/${item.id}/raw`));
            const mime = (item.parts[0].mime || 'image/png').split(';')[0];
            this._lastSig = `image:${GLib.compute_checksum_for_bytes(GLib.ChecksumType.SHA256, bytes)}`;
            this._ignoreUntil = Date.now() + 1000;
            clipboard.set_content(CLIPBOARD, mime, bytes);
            this._clearCache(null);
            return;
        }

        // Files: download into ~/.cache/copia/<id>/ and put file references on the clipboard.
        const dir = Gio.File.new_for_path(GLib.build_filenamev([this._cacheDir, item.id]));
        GLib.mkdir_with_parents(dir.get_path(), 0o700);
        const used = new Set();
        const uris = [];
        for (let n = 0; n < item.parts.length; n++) {
            // eslint-disable-next-line no-await-in-loop
            const bytes = await this._send(this._message('GET', `/clip/${item.id}/parts/${n}`));
            let name = GLib.path_get_basename(item.parts[n].name || `item-${n + 1}`);
            if (name === '.' || name === '..' || name === '/')
                name = `item-${n + 1}`;
            while (used.has(name))
                name = `_${name}`;
            used.add(name);
            const file = dir.get_child(name);
            // eslint-disable-next-line no-await-in-loop
            await writeFile(file, bytes, this._cancellable);
            uris.push(file.get_uri());
        }
        this._lastSig = `files:${uris.join('\n')}`;
        this._ignoreUntil = Date.now() + 1000;
        clipboard.set_content(CLIPBOARD, FILES_TYPE, new GLib.Bytes(encoder.encode(`copy\n${uris.join('\n')}`)));
        this._clearCache(item.id);
    }

    // Keep at most one received item's files on disk (the one on the clipboard now).
    _clearCache(keepId) {
        const root = Gio.File.new_for_path(this._cacheDir);
        try {
            const children = root.enumerate_children('standard::name', Gio.FileQueryInfoFlags.NOFOLLOW_SYMLINKS, null);
            let info;
            while ((info = children.next_file(null)) !== null) {
                if (info.get_name() !== keepId)
                    deleteTree(root.get_child(info.get_name()));
            }
            children.close(null);
        } catch (e) {
            if (!(e instanceof GLib.Error) || !e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND))
                log(`cache cleanup: ${e.message}`);
        }
    }

    // ---------- uploading local copies ----------

    setSyncEnabled(enabled) {
        this._syncEnabled = enabled;
    }

    sendNow() {
        this._lastSig = null;
        this._capture(true).catch(e => {
            if (!isCancelled(e))
                Main.notify('Copia', `Couldn't send the clipboard: ${e.message}`);
        });
    }

    _onLocalChange() {
        if (!this._syncEnabled || Date.now() < this._ignoreUntil)
            return;
        if (this._debounceId)
            GLib.source_remove(this._debounceId);
        this._debounceId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 250, () => {
            this._debounceId = 0;
            this._capture(false).catch(e => {
                if (!isCancelled(e))
                    log(`upload failed: ${e}\n${e.stack ?? ''}`);
            });
            return GLib.SOURCE_REMOVE;
        });
    }

    async _capture(manual) {
        const types = St.Clipboard.get_default().get_mimetypes(CLIPBOARD);
        if (!types.length || types.some(t => SECRET_HINTS.includes(t)))
            return;
        if (this._debug)
            log(`clipboard types: ${types.join(', ')}`);

        // 1. Copied files (Nautilus and most apps offer one of these).
        const uriType = types.includes('text/uri-list') ? 'text/uri-list' : types.includes(FILES_TYPE) ? FILES_TYPE : null;
        if (uriType) {
            const data = await clipboardContent(uriType);
            const uris = data ? parseUriList(decoder.decode(data), uriType === FILES_TYPE) : [];
            if (uris.length && uris.every(u => u.startsWith('file://'))) {
                await this._uploadFiles(uris, manual);
                return;
            }
        }

        // 2. Plain text. Preferred over images when both are offered (e.g. spreadsheet cells).
        if (types.some(t => TEXT_TYPES.includes(t))) {
            const text = await clipboardText();
            if (!text)
                return;
            const sig = `text:${text}`;
            if (sig === this._lastSig)
                return;
            const msg = this._message('POST', '/clip');
            msg.set_request_body_from_bytes('text/plain; charset=utf-8', new GLib.Bytes(encoder.encode(text)));
            await this._send(msg);
            this._lastSig = sig;
            return;
        }

        // 3. Image data (screenshots, "Copy Image" in a browser).
        const imageType = IMAGE_TYPES.find(t => types.includes(t));
        if (imageType) {
            const data = await clipboardContent(imageType);
            if (!data)
                return;
            const bytes = new GLib.Bytes(data);
            const sig = `image:${GLib.compute_checksum_for_bytes(GLib.ChecksumType.SHA256, bytes)}`;
            if (sig === this._lastSig)
                return;
            if (bytes.get_size() > this._maxUpload) {
                Main.notify('Copia', `Image not sent: ${GLib.format_size(bytes.get_size())} is over the server's ${GLib.format_size(this._maxUpload)} limit.`);
                return;
            }
            if (bytes.get_size() > this._autoSyncLimit && !manual) {
                Main.notify('Copia', `Image not synced: ${GLib.format_size(bytes.get_size())} is over the auto-sync limit. Use “Send clipboard now”.`);
                return;
            }
            const msg = this._message('POST', '/clip');
            msg.set_request_body_from_bytes(imageType, bytes);
            await this._send(msg);
            this._lastSig = sig;
        }
    }

    async _uploadFiles(uris, manual) {
        const sig = `files:${uris.join('\n')}`;
        if (sig === this._lastSig)
            return;

        const files = [];
        let total = 0;
        for (const uri of uris) {
            const file = Gio.File.new_for_uri(uri);
            // eslint-disable-next-line no-await-in-loop
            const info = await file.query_info_async('standard::type,standard::size,standard::content-type,standard::display-name',
                Gio.FileQueryInfoFlags.NONE, GLib.PRIORITY_DEFAULT, this._cancellable);
            if (info.get_file_type() === Gio.FileType.DIRECTORY) {
                if (manual)
                    Main.notify('Copia', 'Folders can’t be synced yet — copy the files inside instead.');
                return;
            }
            total += info.get_size();
            files.push({file, name: info.get_display_name(), type: Gio.content_type_get_mime_type(info.get_content_type() ?? '') ?? 'application/octet-stream'});
        }
        if (total > this._maxUpload) {
            Main.notify('Copia', `Not sent: ${GLib.format_size(total)} is over the server's ${GLib.format_size(this._maxUpload)} limit.`);
            return;
        }
        if (total > this._autoSyncLimit && !manual) {
            Main.notify('Copia', `Not synced: ${GLib.format_size(total)} is over the ${GLib.format_size(this._autoSyncLimit)} auto-sync limit. Use “Send clipboard now” to send it anyway.`);
            return;
        }

        const multipart = new Soup.Multipart('multipart/form-data');
        multipart.append_form_string('kind', 'file');
        for (const f of files) {
            // eslint-disable-next-line no-await-in-loop
            const [contents] = await f.file.load_contents_async(this._cancellable);
            multipart.append_form_file('file', f.name, f.type, new GLib.Bytes(contents));
        }
        const msg = Soup.Message.new_from_multipart(`${this._cfg.server}/clip`, multipart);
        msg.request_headers.append('Authorization', `Bearer ${this._cfg.token}`);
        msg.request_headers.append('X-Copia-Device', this._cfg.device);
        await this._send(msg);
        this._lastSig = sig;
    }
}

export default class CopiaExtension extends Extension {
    enable() {
        this._copia = new Copia(this.uuid);
    }

    disable() {
        this._copia?.destroy();
        this._copia = null;
    }
}
