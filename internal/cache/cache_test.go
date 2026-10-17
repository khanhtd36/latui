package cache

import (
	"path/filepath"
	"testing"

	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/notify"
)

func TestSaveLoadPerSite(t *testing.T) {
	SetFile(filepath.Join(t.TempDir(), "latui", "lists.json"))
	if s := Load("a.atlassian.net"); len(s.Direct) != 0 {
		t.Fatalf("missing file should be empty: %+v", s)
	}
	s := Snapshot{
		Site:     "a.atlassian.net",
		Direct:   []notify.Group{{ID: "g1", Unread: true, Head: notify.Notification{Key: "QA-1"}}},
		Assigned: []jira.Row{{Key: "QA-2", Summary: "S"}},
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load("a.atlassian.net")
	if len(got.Direct) != 1 || got.Direct[0].Head.Key != "QA-1" || got.Assigned[0].Summary != "S" || got.Saved.IsZero() {
		t.Errorf("got %+v", got)
	}
	if other := Load("b.atlassian.net"); len(other.Direct) != 0 {
		t.Errorf("another site's lists leaked: %+v", other)
	}
}
