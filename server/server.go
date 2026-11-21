package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxParts     = 1000
	previewRunes = 200
)

var errTooManyParts = errors.New("too many parts")

type Server struct {
	cfg   Config
	store *Store
	hub   *Hub
	log   *slog.Logger
}

func (s *Server) Routes() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("POST /clip", s.handleUpload)
	api.HandleFunc("GET /clip", s.handleList)
	api.HandleFunc("GET /clip/latest", s.handleLatest)
	api.HandleFunc("GET /clip/latest/raw", s.handleLatestRaw)
	api.HandleFunc("GET /clip/{id}", s.handleGet)
	api.HandleFunc("GET /clip/{id}/raw", s.handleRaw)
	api.HandleFunc("GET /clip/{id}/parts/{n}", s.handlePart)
	api.HandleFunc("DELETE /clip/{id}", s.handleDelete)
	api.HandleFunc("GET /events", s.handleEvents)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.Handle("/", s.auth(api))
	return s.logRequests(mux)
}

// ---- middleware ----

func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte(s.cfg.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Copia-Token")
		if got == "" {
			if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
				got = strings.TrimSpace(strings.TrimPrefix(a, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			httpError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer (needed to flush /events).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"bytes", sw.bytes, "dur", time.Since(start).Round(time.Millisecond),
			"remote", r.RemoteAddr, "device", r.Header.Get("X-Copia-Device"))
	})
}

// ---- upload ----

// handleUpload accepts three request shapes:
//
//   - multipart/form-data: a "text" field and/or one or more file fields (any
//     field name), plus optional "device" and "kind" fields. This is what iOS
//     Shortcuts sends with "Request Body: Form".
//   - application/x-www-form-urlencoded: "text", "device" and "kind" fields.
//   - anything else: the raw body is the content. Set Content-Type, and
//     optionally X-Copia-Filename (URL-encoded) for files.
//
// X-Copia-Device and X-Copia-Kind headers override the form fields.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUpload)

	staging, err := s.store.NewStaging()
	if err != nil {
		s.serverError(w, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()

	device := strings.TrimSpace(r.Header.Get("X-Copia-Device"))
	kindHint := strings.TrimSpace(r.Header.Get("X-Copia-Kind"))
	var parts []Part

	ct := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(ct)

	switch mediaType {
	case "multipart/form-data":
		mr, err := r.MultipartReader()
		if err != nil {
			httpError(w, http.StatusBadRequest, "malformed multipart body")
			return
		}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				s.uploadError(w, err)
				return
			}
			switch field, fname := p.FormName(), p.FileName(); {
			case fname != "":
				part, err := s.writePart(staging, len(parts), p, fname, p.Header.Get("Content-Type"))
				if err != nil {
					p.Close()
					s.uploadError(w, err)
					return
				}
				parts = append(parts, part)
			case field == "text":
				part, err := s.writePart(staging, len(parts), p, "", "text/plain; charset=utf-8")
				if err != nil {
					p.Close()
					s.uploadError(w, err)
					return
				}
				parts = append(parts, part)
			case field == "device" || field == "kind":
				b, _ := io.ReadAll(io.LimitReader(p, 256))
				v := strings.TrimSpace(string(b))
				if field == "device" && device == "" {
					device = v
				} else if field == "kind" && kindHint == "" {
					kindHint = v
				}
			default:
				_, _ = io.Copy(io.Discard, p)
			}
			p.Close()
		}

	case "application/x-www-form-urlencoded":
		b, err := io.ReadAll(r.Body)
		if err != nil {
			s.uploadError(w, err)
			return
		}
		form, err := url.ParseQuery(string(b))
		if err != nil {
			httpError(w, http.StatusBadRequest, "malformed form body")
			return
		}
		if device == "" {
			device = strings.TrimSpace(form.Get("device"))
		}
		if kindHint == "" {
			kindHint = strings.TrimSpace(form.Get("kind"))
		}
		if form.Has("text") {
			part, err := s.writePart(staging, 0, strings.NewReader(form.Get("text")), "", "text/plain; charset=utf-8")
			if err != nil {
				s.uploadError(w, err)
				return
			}
			parts = append(parts, part)
		}

	default:
		name := r.Header.Get("X-Copia-Filename")
		if u, err := url.PathUnescape(name); err == nil {
			name = u
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		part, err := s.writePart(staging, 0, r.Body, name, ct)
		if err != nil {
			s.uploadError(w, err)
			return
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		httpError(w, http.StatusBadRequest, "nothing to store: send a body, a \"text\" field, or file fields")
		return
	}
	if len(parts) == 1 && parts[0].Size == 0 {
		httpError(w, http.StatusBadRequest, "content is empty")
		return
	}

	it := &Item{
		ID:        newID(),
		Device:    truncate(device, 64),
		CreatedAt: time.Now().UTC(),
		Parts:     parts,
	}
	it.Kind = classify(parts, kindHint)
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", it.Kind)
	for _, p := range parts {
		it.Size += p.Size
		fmt.Fprintf(h, "%s %d %s\n", p.SHA256, p.Size, p.Name)
	}
	it.Hash = hex.EncodeToString(h.Sum(nil))
	it.AutoSync = it.Size <= s.cfg.AutoSyncLimit
	if it.Kind == "text" {
		it.Preview = readPreview(filepath.Join(staging, partFile(0)), previewRunes)
	}

	// Same content as the current item: don't create a new one. This is what
	// stops a device re-uploading an item it just received from the server.
	if latest := s.store.Latest(); latest != nil && latest.Hash == it.Hash {
		w.Header().Set("X-Copia-Duplicate", "1")
		writeJSON(w, http.StatusOK, latest)
		return
	}

	if err := s.store.Commit(it, staging); err != nil {
		s.serverError(w, err)
		return
	}
	committed = true
	for _, id := range s.store.Prune() {
		s.hub.Publish("delete", map[string]string{"id": id})
	}
	s.hub.Publish("clip", it)
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) writePart(dir string, n int, src io.Reader, name, ctype string) (Part, error) {
	if n >= maxParts {
		return Part{}, errTooManyParts
	}
	f, err := os.OpenFile(filepath.Join(dir, partFile(n)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Part{}, err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Part{}, err
	}
	name = cleanName(name)
	return Part{
		Name:   name,
		Mime:   normalizeMime(ctype, name),
		Size:   size,
		SHA256: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func classify(parts []Part, hint string) string {
	auto := "file"
	if len(parts) == 1 {
		switch p := parts[0]; {
		case p.Name == "" && strings.HasPrefix(p.Mime, "text/"):
			auto = "text"
		case strings.HasPrefix(p.Mime, "image/"):
			auto = "image"
		}
	}
	// "file" can always be forced (e.g. a desktop agent uploading a copied .png
	// file that should paste as a file, not as image data).
	if hint == "file" || hint == auto {
		return hint
	}
	return auto
}

// cleanName reduces a client-supplied filename to a safe base name.
func cleanName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	return truncate(name, 255)
}

func normalizeMime(ctype, name string) string {
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil || mt == "" || mt == "application/octet-stream" {
		if byExt := mime.TypeByExtension(filepath.Ext(name)); byExt != "" {
			return byExt
		}
		return "application/octet-stream"
	}
	return mime.FormatMediaType(mt, params)
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func readPreview(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, n*4)
	k, _ := io.ReadFull(f, buf)
	s := strings.ToValidUTF8(string(buf[:k]), "")
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// ---- reads ----

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	n := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		n = v
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.List(n)})
}

func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request) {
	it := s.store.Latest()
	if it == nil {
		httpError(w, http.StatusNotFound, "clipboard is empty")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) handleLatestRaw(w http.ResponseWriter, r *http.Request) {
	it := s.store.Latest()
	if it == nil {
		httpError(w, http.StatusNotFound, "clipboard is empty")
		return
	}
	s.serveRaw(w, r, it)
}

func (s *Server) itemFromPath(w http.ResponseWriter, r *http.Request) *Item {
	it := s.store.Get(r.PathValue("id"))
	if it == nil {
		httpError(w, http.StatusNotFound, "no such item")
	}
	return it
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if it := s.itemFromPath(w, r); it != nil {
		writeJSON(w, http.StatusOK, it)
	}
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if it := s.itemFromPath(w, r); it != nil {
		s.serveRaw(w, r, it)
	}
}

func (s *Server) handlePart(w http.ResponseWriter, r *http.Request) {
	it := s.itemFromPath(w, r)
	if it == nil {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= len(it.Parts) {
		httpError(w, http.StatusNotFound, "no such part")
		return
	}
	s.servePart(w, r, it, n)
}

// serveRaw returns the content itself: the single part as-is, or a zip of all parts.
func (s *Server) serveRaw(w http.ResponseWriter, r *http.Request, it *Item) {
	if len(it.Parts) == 1 {
		s.servePart(w, r, it, 0)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", disposition("clipboard-"+it.CreatedAt.Format("20060102-150405")+".zip"))
	w.Header().Set("X-Copia-Id", it.ID)
	w.Header().Set("X-Copia-Kind", it.Kind)
	if r.Method == http.MethodHead {
		return
	}
	zw := zip.NewWriter(w)
	used := map[string]int{}
	for i, p := range it.Parts {
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("item-%d", i+1)
		}
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: uniqueName(used, name), Method: zip.Deflate, Modified: it.CreatedAt})
		if err != nil {
			s.log.Warn("zip", "err", err)
			return
		}
		f, err := os.Open(s.store.PartPath(it.ID, i))
		if err != nil {
			s.log.Warn("zip open", "err", err)
			return
		}
		_, err = io.Copy(fw, f)
		f.Close()
		if err != nil {
			return // client went away
		}
	}
	_ = zw.Close()
}

func uniqueName(used map[string]int, name string) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	ext := filepath.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), used[name], ext)
}

func (s *Server) servePart(w http.ResponseWriter, r *http.Request, it *Item, n int) {
	p := it.Parts[n]
	f, err := os.Open(s.store.PartPath(it.ID, n))
	if err != nil {
		httpError(w, http.StatusGone, "item content is no longer available")
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", p.Mime)
	h.Set("X-Copia-Id", it.ID)
	h.Set("X-Copia-Kind", it.Kind)
	h.Set("ETag", `"`+p.SHA256+`"`)
	if p.Name != "" {
		h.Set("Content-Disposition", disposition(p.Name))
	}
	http.ServeContent(w, r, "", it.CreatedAt, f)
}

func disposition(name string) string {
	if v := mime.FormatMediaType("attachment", map[string]string{"filename": name}); v != "" {
		return v
	}
	return "attachment"
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.store.Delete(id) {
		httpError(w, http.StatusNotFound, "no such item")
		return
	}
	s.hub.Publish("delete", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

// ---- live events ----

// handleEvents streams Server-Sent Events:
//
//	event: hello   data: {"latest": Item|null, "autoSyncLimit": n}   (on connect)
//	event: clip    data: Item                                         (new item)
//	event: delete  data: {"id": "..."}                                (item removed)
//
// plus a ": ping" comment every 25s to keep connections alive.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch := s.hub.Subscribe() // subscribe before reading latest so nothing is missed
	defer s.hub.Unsubscribe(ch)

	hello, _ := json.Marshal(map[string]any{"latest": s.store.Latest(), "autoSyncLimit": s.cfg.AutoSyncLimit})
	if _, err := fmt.Fprintf(w, "retry: 3000\nevent: hello\ndata: %s\n\n", hello); err != nil {
		return
	}
	if rc.Flush() != nil {
		return
	}

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil || rc.Flush() != nil {
				return
			}
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

func (s *Server) pruneLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, id := range s.store.Prune() {
				s.hub.Publish("delete", map[string]string{"id": id})
			}
		}
	}
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	s.log.Error("internal", "err", err)
	httpError(w, http.StatusInternalServerError, "internal error")
}

func (s *Server) uploadError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		httpError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("upload exceeds %d bytes", s.cfg.MaxUpload))
	case errors.Is(err, errTooManyParts):
		httpError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("more than %d files", maxParts))
	default:
		s.log.Warn("upload failed", "err", err)
		httpError(w, http.StatusBadRequest, "upload failed")
	}
}
