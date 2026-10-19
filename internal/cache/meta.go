package cache

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/khanhtd36/latui/internal/jira"
)

// Meta is what the create form needs about one project: its issue types,
// their statuses and each type's create fields. It changes rarely.
type Meta struct {
	Types    []jira.IssueType               `json:"types,omitempty"`
	Statuses map[string][]jira.StatusOption `json:"statuses,omitempty"` // by type ID
	Fields   map[string][]jira.CreateField  `json:"fields,omitempty"`   // by type ID
}

// Metas are the create form's metadata of one Site, by project key.
type Metas struct {
	Site     string           `json:"site"`
	Projects map[string]*Meta `json:"projects"`
}

var metaFile = func() (string, error) {
	f, err := file()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(f), "createmeta.json"), nil
}

// LoadMetas reads the site's create metadata; anything unreadable is empty.
func LoadMetas(site string) *Metas {
	m := &Metas{Site: site, Projects: map[string]*Meta{}}
	f, err := metaFile()
	if err != nil {
		return m
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return m
	}
	var got Metas
	if json.Unmarshal(b, &got) != nil || got.Site != site || got.Projects == nil {
		return m
	}
	return &got
}

// Project is a project's metadata, made empty on first use.
func (m *Metas) Project(key string) *Meta {
	p, ok := m.Projects[key]
	if !ok {
		p = &Meta{Statuses: map[string][]jira.StatusOption{}, Fields: map[string][]jira.CreateField{}}
		m.Projects[key] = p
	}
	if p.Statuses == nil {
		p.Statuses = map[string][]jira.StatusOption{}
	}
	if p.Fields == nil {
		p.Fields = map[string][]jira.CreateField{}
	}
	return p
}

// Has reports whether a project's metadata is known.
func (m *Metas) Has(key string) bool {
	p, ok := m.Projects[key]
	return ok && len(p.Types) > 0
}

// Save writes the metadata.
func (m *Metas) Save() error {
	f, err := metaFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}
