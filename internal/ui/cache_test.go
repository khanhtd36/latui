package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/cache"
	"github.com/khanhtd36/latui/internal/notify"
)

// slowFeed holds its answers back, as the real feed takes seconds.
type slowFeed struct{ *fakeFeed }

func (s slowFeed) Feed(context.Context, notify.Category, bool, int, string) (notify.Page, error) {
	return notify.Page{}, context.DeadlineExceeded
}

func TestCachedListsShowAtStart(t *testing.T) {
	snap := cache.Snapshot{
		Site:   "x.atlassian.net",
		Direct: []notify.Group{{ID: "c1", Unread: true, Head: notify.Notification{Time: time.Now(), Message: "Cached mention", Title: "From last run"}}},
	}
	if err := snap.Save(); err != nil {
		t.Fatal(err)
	}
	m := New(issuesJira(), slowFeed{sampleFeed()}, time.Minute)
	m = run(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	// Only the start: the feed's (failing) answers are dropped here.
	next, _ := m.Update(m.start()())
	m = next.(Model)
	m = run(t, m, key("2"))
	s := screen(m)
	if !strings.Contains(s, "Cached mention") || !strings.Contains(s, "cached ") {
		t.Fatalf("cached list should show at once:\n%s", s)
	}
}

func TestFreshListReplacesCacheAndSaves(t *testing.T) {
	snap := cache.Snapshot{Site: "x.atlassian.net", Direct: []notify.Group{{ID: "old", Unread: true, Head: notify.Notification{Message: "Stale one"}}}}
	if err := snap.Save(); err != nil {
		t.Fatal(err)
	}
	m := started(t, issuesJira(), sampleFeed())
	if s := screen(m); strings.Contains(s, "Stale one") || strings.Contains(s, "cached ") {
		t.Fatalf("fresh list should replace the cached one:\n%s", s)
	}
	saved := cache.Load("x.atlassian.net")
	if len(saved.Direct) != 1 || saved.Direct[0].ID != "g1" || len(saved.Assigned) != 4 {
		t.Errorf("saved %+v", saved)
	}
}
