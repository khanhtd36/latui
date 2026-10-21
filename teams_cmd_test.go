package main

import (
	"strings"
	"testing"
)

func TestReadTokenFromAPipe(t *testing.T) {
	got, err := readTokenFrom(strings.NewReader("  abc123\r\n"))
	if err != nil || got != "abc123" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := readTokenFrom(strings.NewReader("  \n")); err == nil {
		t.Error("an empty pipe should be an error")
	}
}

func TestTeamsCommandNeedsLoginOrLogout(t *testing.T) {
	for _, args := range [][]string{nil, {"nonsense"}} {
		if err := teamsCommand(args); err == nil || !strings.Contains(err.Error(), "latui teams login") {
			t.Errorf("%v: %v", args, err)
		}
	}
}
