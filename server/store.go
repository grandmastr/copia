package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Part is one piece of clipboard content: the text, the image, or one file.
type Part struct {
	Name   string `json:"name,omitempty"`
	Mime   string `json:"mime"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Item is one clipboard entry.
type Item struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // text | image | file
	Device    string    `json:"device,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
	Hash      string    `json:"hash"`     // content hash, used to skip duplicates and stop sync loops
	AutoSync  bool      `json:"autoSync"` // false when larger than the auto-sync limit: fetch only on paste
	Preview   string    `json:"preview,omitempty"`
	Parts     []Part    `json:"parts"`
}

var idRe = regexp.MustCompile(`^[0-9a-f]{16}-[0-9a-f]{8}$`)

// newID returns a time-sortable unique ID.
func newID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%016x-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

func partFile(n int) string { return fmt.Sprintf("part-%d", n) }

// Store keeps items on disk under <root>/items/<id>/ (meta.json + part-N files)
// with an in-memory index ordered oldest to newest.
type Store struct {
	itemsDir string
	tmpDir   string
	maxItems int
	maxAge   time.Duration

	mu    sync.RWMutex
	items []*Item
}

func OpenStore(root string, maxItems int, maxAge time.Duration) (*Store, error) {
	s := &Store{
		itemsDir: filepath.Join(root, "items"),
		tmpDir:   filepath.Join(root, "tmp"),
		maxItems: maxItems,
		maxAge:   maxAge,
	}
	_ = os.RemoveAll(s.tmpDir) // leftovers from interrupted uploads
	for _, d := range []string{s.itemsDir, s.tmpDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(s.itemsDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !idRe.MatchString(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.itemsDir, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var it Item
		if json.Unmarshal(b, &it) != nil || it.ID != e.Name() {
			continue
		}
		s.items = append(s.items, &it)
	}
	sort.Slice(s.items, func(i, j int) bool { return s.items[i].ID < s.items[j].ID })
	s.Prune()
	return s, nil
}

// NewStaging creates a private directory for an upload in progress.
func (s *Store) NewStaging() (string, error) { return os.MkdirTemp(s.tmpDir, "up-") }

// Commit writes the item's metadata into its staging directory and moves it into place.
func (s *Store) Commit(it *Item, staging string) error {
	b, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "meta.json"), b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(staging, filepath.Join(s.itemsDir, it.ID)); err != nil {
		return err
	}
	s.mu.Lock()
	s.items = append(s.items, it)
	s.mu.Unlock()
	return nil
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

func (s *Store) Latest() *Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.items) == 0 {
		return nil
	}
	return s.items[len(s.items)-1]
}

func (s *Store) Get(id string) *Item {
	if !idRe.MatchString(id) {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// List returns up to n items, newest first (n <= 0 means all).
func (s *Store) List(n int) []*Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || n > len(s.items) {
		n = len(s.items)
	}
	out := make([]*Item, 0, n)
	for i := len(s.items) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, s.items[i])
	}
	return out
}

func (s *Store) PartPath(id string, n int) string {
	return filepath.Join(s.itemsDir, id, partFile(n))
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.items {
		if it.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			_ = os.RemoveAll(filepath.Join(s.itemsDir, id))
			return true
		}
	}
	return false
}

// Prune drops items beyond maxItems or older than maxAge. The newest item is
// always kept: it stays until something replaces it.
func (s *Store) Prune() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) <= 1 {
		return nil
	}
	cutoff := time.Now().Add(-s.maxAge)
	keepFrom := 0
	if len(s.items) > s.maxItems {
		keepFrom = len(s.items) - s.maxItems
	}
	kept := make([]*Item, 0, len(s.items))
	var removed []string
	for i, it := range s.items {
		newest := i == len(s.items)-1
		if !newest && (i < keepFrom || it.CreatedAt.Before(cutoff)) {
			removed = append(removed, it.ID)
			continue
		}
		kept = append(kept, it)
	}
	s.items = kept
	for _, id := range removed {
		_ = os.RemoveAll(filepath.Join(s.itemsDir, id))
	}
	return removed
}
