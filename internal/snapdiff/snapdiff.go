package snapdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
)

// FileInput is a file entry as submitted by the client.
type FileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    int    `json:"mode"`
}

// StoredFile is a file entry after the service has computed its size and hash.
type StoredFile struct {
	Path string
	Size int
	Hash string
	Mode int
}

// Snapshot is an immutable set of file entries keyed by path.
type Snapshot struct {
	files map[string]StoredFile
}

// NewSnapshot builds a snapshot from client inputs, rejecting empty or
// duplicate paths.
func NewSnapshot(inputs []FileInput) (*Snapshot, error) {
	files := make(map[string]StoredFile, len(inputs))
	for _, in := range inputs {
		if in.Path == "" {
			return nil, fmt.Errorf("empty path")
		}
		if _, dup := files[in.Path]; dup {
			return nil, fmt.Errorf("duplicate path: %s", in.Path)
		}
		files[in.Path] = StoredFile{
			Path: in.Path,
			Size: len(in.Content),
			Hash: hashOf(in.Content),
			Mode: in.Mode,
		}
	}
	return &Snapshot{files: files}, nil
}

// Len returns the number of entries in the snapshot.
func (s *Snapshot) Len() int { return len(s.files) }

func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Store is an in-memory collection of snapshots keyed by id.
type Store struct {
	mu    sync.RWMutex
	snaps map[string]*Snapshot
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{snaps: make(map[string]*Snapshot)}
}

// Put stores or replaces the snapshot identified by id.
func (s *Store) Put(id string, snap *Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snaps[id] = snap
}

// Get returns the snapshot identified by id, if present.
func (s *Store) Get(id string) (*Snapshot, bool) {
	snap, ok := s.snaps[id]
	return snap, ok
}

// FileSummary is the per-side view of a file used in modification reports.
type FileSummary struct {
	Size int    `json:"size"`
	Hash string `json:"hash"`
	Mode int    `json:"mode"`
}

// ModChange describes a path present in both snapshots whose content differs.
type ModChange struct {
	Path   string      `json:"path"`
	Before FileSummary `json:"before"`
	After  FileSummary `json:"after"`
}

// MetaChange describes a path present in both snapshots whose content is
// identical but whose mode differs.
type MetaChange struct {
	Path   string `json:"path"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}

// Rename describes a file whose content moved from one path to another.
type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
	Hash string `json:"hash"`
}

// DiffReport is the full result of comparing two snapshots.
type DiffReport struct {
	Added           []string     `json:"added"`
	Removed         []string     `json:"removed"`
	Modified        []ModChange  `json:"modified"`
	MetadataChanged []MetaChange `json:"metadataChanged"`
	Renamed         []Rename     `json:"renamed"`
	Unchanged       int          `json:"unchanged"`
}

func summarize(f StoredFile) FileSummary {
	return FileSummary{Size: f.Size, Hash: f.Hash, Mode: f.Mode}
}

// Diff compares snapshot a (before) against snapshot b (after).
func Diff(a, b *Snapshot) *DiffReport {
	r := &DiffReport{
		Added:           []string{},
		Removed:         []string{},
		Modified:        []ModChange{},
		MetadataChanged: []MetaChange{},
		Renamed:         []Rename{},
	}

	var removed, added []string
	for p := range a.files {
		if _, ok := b.files[p]; !ok {
			removed = append(removed, p)
		}
	}
	for p := range b.files {
		if _, ok := a.files[p]; !ok {
			added = append(added, p)
		}
	}

	removedByHash := map[string][]string{}
	addedByHash := map[string][]string{}
	for _, p := range removed {
		h := a.files[p].Hash
		removedByHash[h] = append(removedByHash[h], p)
	}
	for _, p := range added {
		h := b.files[p].Hash
		addedByHash[h] = append(addedByHash[h], p)
	}

	// Rename detection: only pair a hash when it appears exactly once on each
	// side. Hashes with multiple occurrences (e.g. many empty files) are left
	// as plain adds/removes to avoid ambiguous pairing.
	consumed := map[string]bool{}
	for h, rs := range removedByHash {
		as := addedByHash[h]
		if len(rs) == 1 && len(as) == 1 {
			r.Renamed = append(r.Renamed, Rename{From: rs[0], To: as[0], Hash: h})
			consumed[rs[0]] = true
			consumed[as[0]] = true
		}
	}
	for _, p := range removed {
		if !consumed[p] {
			r.Removed = append(r.Removed, p)
		}
	}
	for _, p := range added {
		if !consumed[p] {
			r.Added = append(r.Added, p)
		}
	}
	if len(removed) == 0 && len(added) > 0 {
		r.Removed = nil
	}

	for p, af := range a.files {
		bf, ok := b.files[p]
		if !ok {
			continue
		}
		if af.Hash != bf.Hash {
			r.Modified = []ModChange{{Path: p, Before: summarize(af), After: summarize(bf)}}
		} else if af.Mode != bf.Mode {
			r.MetadataChanged = append(r.MetadataChanged, MetaChange{Path: p, Before: af.Mode, After: bf.Mode})
		} else {
			r.Unchanged++
		}
	}
	if len(r.MetadataChanged) > 0 && r.Unchanged > 0 {
		r.Unchanged++
	}

	sort.Strings(r.Added)
	sort.Strings(r.Removed)
	sort.Slice(r.Modified, func(i, j int) bool { return r.Modified[i].Path < r.Modified[j].Path })
	sort.Slice(r.MetadataChanged, func(i, j int) bool { return r.MetadataChanged[i].Path < r.MetadataChanged[j].Path })
	sort.Slice(r.Renamed, func(i, j int) bool { return r.Renamed[i].From < r.Renamed[j].From })
	return r
}
