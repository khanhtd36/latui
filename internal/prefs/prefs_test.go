package prefs

import (
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	SetFile(filepath.Join(t.TempDir(), "latui", "config.json"))
	if p := Load(); p.CreateAnother || p.Collapsed != nil {
		t.Fatalf("missing file should load empty, got %+v", p)
	}
	want := Prefs{CreateAnother: true, LastProject: "QA", LastType: "10004", Collapsed: map[string]bool{"Deferred": true}}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !got.CreateAnother || got.LastProject != "QA" || got.LastType != "10004" || !got.Collapsed["Deferred"] {
		t.Errorf("got %+v", got)
	}
}
