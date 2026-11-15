// internal/store/annotation.go — Per-symbol, per-version annotation store.
// Annotations are stored outside the immutable Index so they survive
// re-indexing. They are keyed by (version, symbolName, key).
// Sources: "auto" (indexer), "plugin:name", "user" (frontend).
//
// Part of github.com/hanemile/kscope
package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Well-known annotation keys ────────────────────────────────────────────────

const (
	// Auto-detected by indexer
	AnnCopyFromUser  = "copy_from_user_site"  // line contains copy_from_user()
	AnnCopyToUser    = "copy_to_user_site"
	AnnSizeofCall    = "sizeof_call"          // sizeof(struct_name) usage
	AnnSizeofTooSmall = "sizeof_too_small"    // sizeof misses embedded ptr fields
	AnnUserControlled = "user_controlled_size" // size determined from userspace input
	AnnStaticSize    = "static_size"          // struct has fixed compile-time size
	AnnDynamicSize   = "dynamic_size"         // struct has trailing flexible array

	// Set by plugins or users
	AnnStructLayout   = "struct_layout"       // JSON: {fields:[{name,offset,size}]}
	AnnKernelSide     = "kernel_side_struct"
	AnnUserspaceSide  = "userspace_side_struct"
	AnnChecked        = "checked"             // has input validation after copy
	AnnUnchecked      = "unchecked"           // missing validation
	AnnNote           = "note"                // free-form note

	// Solver results
	AnnSolverResult   = "solver_result"       // JSON from solver plugin
)

// Annotation is one annotation on one symbol in one kernel version.
type Annotation struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	Symbol    string    `json:"symbol"`
	File      string    `json:"file,omitempty"`
	Line      int       `json:"line,omitempty"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	ValueType string    `json:"value_type"` // string|int|bool|json
	Note      string    `json:"note,omitempty"`
	Source    string    `json:"source"` // auto|plugin:name|user
	CreatedAt time.Time `json:"created_at"`
}

// AnnotationStore holds all annotations, thread-safe.
// Stored outside Index so they survive re-indexing.
type AnnotationStore struct {
	mu   sync.RWMutex
	data map[string][]Annotation // key: version+":"+symbol
}

func NewAnnotationStore() *AnnotationStore {
	return &AnnotationStore{data: make(map[string][]Annotation)}
}

func annKey(version, symbol string) string {
	return version + ":" + symbol
}

// Add inserts one annotation. If an annotation with the same ID exists it is replaced.
func (s *AnnotationStore) Add(a Annotation) {
	if a.ID == "" {
		a.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	k := annKey(a.Version, a.Symbol)
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.data[k]
	for i, existing := range list {
		if existing.ID == a.ID {
			list[i] = a
			s.data[k] = list
			return
		}
	}
	s.data[k] = append(list, a)
}

// AddBatch inserts many annotations efficiently.
func (s *AnnotationStore) AddBatch(anns []Annotation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range anns {
		if a.ID == "" { a.ID = fmt.Sprintf("%d-%s", time.Now().UnixNano(), a.Symbol) }
		if a.CreatedAt.IsZero() { a.CreatedAt = time.Now() }
		k := annKey(a.Version, a.Symbol)
		s.data[k] = append(s.data[k], a)
	}
}

// Get returns all annotations for a symbol in a version.
func (s *AnnotationStore) Get(version, symbol string) []Annotation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Annotation{}, s.data[annKey(version, symbol)]...)
}

// GetCrossVersion returns annotations for the same symbol across all versions.
func (s *AnnotationStore) GetCrossVersion(symbol string) map[string][]Annotation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string][]Annotation{}
	for k, anns := range s.data {
		for _, a := range anns {
			if a.Symbol == symbol {
				out[k] = append(out[k], a)
				break
			}
		}
		_ = k
	}
	// Better: scan by prefix impossible with current key scheme; use full scan
	result := map[string][]Annotation{}
	for _, anns := range s.data {
		for _, a := range anns {
			if a.Symbol == symbol {
				result[a.Version] = append(result[a.Version], a)
			}
		}
	}
	return result
}

// Delete removes one annotation by ID.
func (s *AnnotationStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, list := range s.data {
		for i, a := range list {
			if a.ID == id {
				s.data[k] = append(list[:i], list[i+1:]...)
				return true
			}
		}
	}
	return false
}

// ListForVersion returns all annotations for all symbols in a version.
func (s *AnnotationStore) ListForVersion(version string) []Annotation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Annotation
	for _, anns := range s.data {
		for _, a := range anns {
			if a.Version == version {
				out = append(out, a)
			}
		}
	}
	return out
}

// ForFile returns annotations for all symbols defined in a specific file.
func (s *AnnotationStore) ForFile(version, file string) map[string][]Annotation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string][]Annotation{}
	for _, anns := range s.data {
		for _, a := range anns {
			if a.Version == version && a.File == file {
				out[a.Symbol] = append(out[a.Symbol], a)
			}
		}
	}
	return out
}
