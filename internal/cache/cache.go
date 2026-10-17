// Package cache keeps the last lists latui showed, so the next start can
// show them at once while fresh ones load.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/notify"
)

// Snapshot is the last lists of one Site.
type Snapshot struct {
	Site     string         `json:"site"`
	Saved    time.Time      `json:"saved"`
	Direct   []notify.Group `json:"direct,omitempty"` // unread only
	Watching []notify.Group `json:"watching,omitempty"`
	Assigned []jira.Row     `json:"assigned,omitempty"`
	Reported []jira.Row     `json:"reported,omitempty"`
}

// file is where the snapshot lives; tests point it elsewhere.
var file = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "latui", "lists.json"), nil
}

// SetFile points the cache at path, for tests.
func SetFile(path string) { file = func() (string, error) { return path, nil } }

// Load reads the snapshot for site; a missing, broken or other site's
// file is an empty snapshot.
func Load(site string) Snapshot {
	empty := Snapshot{Site: site}
	f, err := file()
	if err != nil {
		return empty
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return empty
	}
	var s Snapshot
	if json.Unmarshal(b, &s) != nil || s.Site != site {
		return empty
	}
	return s
}

// Save writes the snapshot, stamped now.
func (s Snapshot) Save() error {
	f, err := file()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	s.Saved = time.Now()
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// Write then rename, so a crash never leaves half a file.
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}
