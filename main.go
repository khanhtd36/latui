package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khanhtd36/latui/internal/auth"
	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/notify"
	"github.com/khanhtd36/latui/internal/termprobe"
	"github.com/khanhtd36/latui/internal/ui"
	"github.com/khanhtd36/latui/internal/update"
)

// Set by goreleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	interval := flag.Duration("interval", time.Minute, "how often notifications refresh")
	showVersion := flag.Bool("version", false, "print version and exit")
	symbols := flag.Bool("symbols", false, "draw markers as symbols (● ▌) without checking the terminal")
	ascii := flag.Bool("ascii", false, "draw markers as ASCII (*)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("latui %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	exe, _ := os.Executable()
	update.Cleanup(exe) // the copy a previous update moved aside
	if flag.Arg(0) == "update" {
		if err := selfUpdate(exe); err != nil {
			fmt.Fprintln(os.Stderr, "latui:", err)
			os.Exit(1)
		}
		return
	}
	ui.SetVersion(version)
	ui.UseSymbols(chooseSymbols(*symbols, *ascii, os.Getenv("LATUI_SYMBOLS")))

	restore := os.Getenv(restoreEnv)
	_ = os.Unsetenv(restoreEnv)
	for {
		state, err := run(max(15*time.Second, *interval), restore)
		if err != nil {
			fmt.Fprintln(os.Stderr, "latui:", err)
			os.Exit(1)
		}
		if state == "" {
			return
		}
		// The user restarted into the update just installed.
		err = restart(exe, append(os.Environ(), restoreEnv+"="+state))
		fmt.Fprintln(os.Stderr, "latui: restart into the update:", err, "(carrying on with this version)")
		time.Sleep(2 * time.Second)
		restore = state
	}
}

// restoreEnv carries the page, tab and issue to a restarted latui.
const restoreEnv = "LATUI_RESTORE"

// run shows latui, from where restore says, and returns the state to restart
// with when the user restarts into an installed update.
func run(interval time.Duration, restore string) (string, error) {
	creds := auth.Default(os.Getenv)
	model := ui.New(jira.New(creds), notify.New(creds), interval).Restore(restore)
	final, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return "", err
	}
	if m, ok := final.(ui.Model); ok {
		if state, restart := m.RestartState(); restart {
			return state, nil
		}
	}
	return "", nil
}

// selfUpdate is `latui update`: it lists what newer releases change and
// installs the latest, unless a package manager owns this copy.
func selfUpdate(exe string) error {
	ctx := context.Background()
	rs, err := update.Newer(ctx, version)
	if err != nil {
		return err
	}
	if len(rs) == 0 {
		fmt.Printf("latui %s is the latest.\n", version)
		return nil
	}
	fmt.Printf("latui %s → %s\n", version, rs[0].Version())
	for _, c := range update.Changes(rs) {
		fmt.Println("  " + c)
	}
	how := update.HowInstalled(exe)
	if cmd := how.Command(); cmd != "" {
		fmt.Println("This copy is managed elsewhere; update it with:\n  " + cmd)
		return nil
	}
	last := ""
	err = update.Install(ctx, rs[0], exe, func(s update.Step) {
		if line := s.String(); line != last {
			last = line
			fmt.Print("\r" + line + "\x1b[K")
		}
	})
	if last != "" {
		fmt.Print("\r\x1b[K")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Updated to %s.\n", rs[0].Version())
	return nil
}

// chooseSymbols decides between marker symbols and ASCII: a flag, then
// LATUI_SYMBOLS (1 or 0), then whether the terminal draws the symbols one
// column each.
func chooseSymbols(symbols, ascii bool, env string) bool {
	switch {
	case ascii:
		return false
	case symbols:
		return true
	case env == "1":
		return true
	case env == "0":
		return false
	}
	w, ok := termprobe.Width(ui.SymbolSample, 300*time.Millisecond)
	return ok && w == utf8.RuneCountInString(ui.SymbolSample)
}
