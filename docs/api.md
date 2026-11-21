# API reference

Base URL: `http://<server>:8787`. Every endpoint except `/healthz` requires the token, as either header:

```
Authorization: Bearer <token>
X-Copia-Token: <token>
```

Errors are JSON: `{"error": "message"}`.

## Items

An item is one clipboard entry:

```json
{
  "id": "18ea1dda080197ed-a19893fb",
  "kind": "file",
  "device": "my-laptop",
  "createdAt": "2026-11-21T16:57:03.123Z",
  "size": 5012,
  "hash": "9f2c…",
  "autoSync": true,
  "parts": [
    { "name": "README.md", "mime": "text/markdown", "size": 5012, "sha256": "4b1e…" }
  ]
}
```

| Field | Meaning |
|---|---|
| `id` | Time-sortable unique ID |
| `kind` | `text`, `image` or `file` |
| `device` | Who sent it (from `X-Copia-Device`) |
| `size` | Total bytes across parts |
| `hash` | Content hash of the whole item; identical content gives the same hash |
| `autoSync` | `false` when larger than `COPIA_AUTOSYNC_LIMIT`: clients should fetch only when asked |
| `preview` | First 200 characters (text items only) |
| `parts` | The text, the image, or one entry per file |

## Endpoints

| Method & path | Description |
|---|---|
| `POST /clip` | Upload an item. `201` with the item; `200` and `X-Copia-Duplicate: 1` if it matches the current item; `413` if over the limit |
| `GET /clip/latest` | The current item's metadata; `404` if empty |
| `GET /clip/latest/raw` | The current item's content: the text, image or file itself, or a zip of several files |
| `GET /clip?limit=20` | Items, newest first |
| `GET /clip/{id}` | One item's metadata |
| `GET /clip/{id}/raw` | One item's content (as above) |
| `GET /clip/{id}/parts/{n}` | One part's content, with `Content-Disposition` filename and an `ETag` (`If-None-Match` gives `304`) |
| `DELETE /clip/{id}` | Remove an item (`204`) |
| `GET /events` | Live events (see below) |
| `GET /healthz` | `ok`, no token needed |

Content responses also carry `X-Copia-Id` and `X-Copia-Kind` headers.

## Uploading

Three request shapes are accepted on `POST /clip`.

**Raw body:** the body is the content. Set `Content-Type`; for files, add a URL-encoded filename.

```bash
curl -H "Authorization: Bearer $T" -H "Content-Type: text/plain; charset=utf-8" \
     --data-binary 'hello' http://server:8787/clip

curl -H "Authorization: Bearer $T" -H "Content-Type: image/png" \
     -H "X-Copia-Filename: screen%20shot.png" --data-binary @shot.png http://server:8787/clip
```

**Multipart form** (what iOS Shortcuts sends): a `text` field, and/or any number of file fields.

```bash
curl -H "Authorization: Bearer $T" -F device=phone -F file=@a.pdf -F file=@b.jpg http://server:8787/clip
```

**URL-encoded form:**

```bash
curl -H "Authorization: Bearer $T" -d 'text=hello&device=laptop' http://server:8787/clip
```

Optional on all three:

| Header | Form field | Meaning |
|---|---|---|
| `X-Copia-Device` | `device` | Name of the sending device |
| `X-Copia-Kind: file` | `kind=file` | Treat a single image as a file (paste as a file, not image data) |

How the kind is decided: a single unnamed `text/*` part is `text`; a single `image/*` part is `image`; anything else is `file`. File names are reduced to a safe base name (`../../etc/x` becomes `x`).

## Live events

`GET /events` is a [Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html) stream:

| Event | Data | When |
|---|---|---|
| `hello` | `{"latest": Item or null, "autoSyncLimit": n, "maxUpload": n}` | On connect |
| `clip` | Item | A new item was stored |
| `delete` | `{"id": "…"}` | An item was removed (replaced, expired or deleted) |

The server sends a `: ping` comment every 25 seconds and suggests `retry: 3000`. A client that falls behind is disconnected and should reconnect; the `hello` event lets it catch up.

```bash
curl -N -H "Authorization: Bearer $T" http://server:8787/events
```
