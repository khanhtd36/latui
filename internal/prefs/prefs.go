// Package prefs keeps what latui remembers between runs.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Prefs is the saved file.
type Prefs struct {
	// CreateAnother keeps the create form open after creating an issue.
	CreateAnother bool `json:"createAnother,omitempty"`
	// LastProject and LastType are what the create form starts with.
	LastProject string `json:"lastProject,omitempty"`
	LastType    string `json:"lastType,omitempty"`
	// Collapsed are the My issues Sections closed, by status name; nil
	// means the defaults (Fixed and Deferred closed).
	Collapsed map[string]bool `json:"collapsed,omitempty"`
	// Mentions are who was mentioned lately, newest first.
	Mentions []Mention `json:"mentions,omitempty"`
}

// file is where the prefs live; tests point it elsewhere.
var file = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "latui", "config.json"), nil
}

// SetFile points the prefs at path, for tests.
func SetFile(path string) { file = func() (string, error) { return path, nil } }

// Load reads the prefs; a missing or broken file is empty prefs.
func Load() Prefs {
	var p Prefs
	if f, err := file(); err == nil {
		if b, err := os.ReadFile(f); err == nil {
			_ = json.Unmarshal(b, &p)
		}
	}
	return p
}

// Save writes the prefs, creating the folder.
func (p Prefs) Save() error {
	f, err := file()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f, b, 0o600)
}

// Mention is a person or a team mentioned lately, offered first in the next
// mention picker.
type Mention struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Team bool   `json:"team,omitempty"`
}

// maxMentions is how many recent mentions are kept.
const maxMentions = 8

// RememberMention puts m first among the recent mentions, once.
func (p *Prefs) RememberMention(m Mention) {
	out := []Mention{m}
	for _, r := range p.Mentions {
		if r.ID != m.ID && len(out) < maxMentions {
			out = append(out, r)
		}
	}
	p.Mentions = out
}
