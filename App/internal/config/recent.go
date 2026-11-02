package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// ---------------------------------------------------------------------------
// Recent models per provider (R-101): the user config dir, never the project
// ---------------------------------------------------------------------------

const (
	recentFile = "recent.json"
	recentCap  = 10 // ids kept per provider
)

// Recent remembers the models used on each provider, newest first.
type Recent struct {
	path string
	m    struct {
		Models map[string][]string `json:"models"`
	}
}

// LoadRecent reads <dir>/recent.json; a missing file is an empty store.
func LoadRecent(dir string) (*Recent, error) {
	r := &Recent{path: filepath.Join(dir, recentFile)}
	r.m.Models = map[string][]string{}
	data, err := os.ReadFile(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("recent: %w", err)
	}
	if err := json.Unmarshal(data, &r.m); err != nil {
		return nil, fmt.Errorf("recent: parse %s: %w", r.path, err)
	}
	if r.m.Models == nil {
		r.m.Models = map[string][]string{}
	}
	return r, nil
}

// Models lists the ids used on provider, newest first (a copy).
func (r *Recent) Models(provider string) []string {
	return slices.Clone(r.m.Models[provider])
}

// First is the model a switch to provider starts with: its last used id, else fallback (R-100).
func (r *Recent) First(provider, fallback string) string {
	if list := r.m.Models[provider]; len(list) > 0 {
		return list[0]
	}
	return fallback
}

// Use moves id to the front of the provider's list (added when new, the list capped) and saves.
func (r *Recent) Use(provider, id string) error {
	list := slices.DeleteFunc(r.Models(provider), func(s string) bool { return s == id })
	list = append([]string{id}, list...)
	if len(list) > recentCap {
		list = list[:recentCap]
	}
	r.m.Models[provider] = list
	return writeJSON(r.path, r.m)
}
