package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/khanhtd36/latui/internal/auth"
	"github.com/khanhtd36/latui/internal/teams"
)

const teamsUsage = `usage:
  latui teams login [--email you@example.com]   paste an API token (hidden), or pipe it in
  latui teams logout                            forget the stored token

Teams need an API token because acli's sign-in can't read them. Create one at
https://id.atlassian.com/manage-profile/security/api-tokens . It is checked,
then kept in the OS secure store. LATUI_TEAMS_TOKEN (and LATUI_TEAMS_EMAIL)
in the environment take priority over it`

// teamsCommand is `latui teams login` and `latui teams logout`.
func teamsCommand(args []string) error {
	if len(args) == 0 || args[0] != "login" && args[0] != "logout" {
		return errors.New(teamsUsage)
	}
	client := teams.New(auth.Default(os.Getenv), os.Getenv, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if args[0] == "logout" {
		if err := client.Logout(ctx); err != nil {
			return err
		}
		fmt.Println("The stored Teams token is removed.")
		return nil
	}
	fs := flag.NewFlagSet("teams login", flag.ContinueOnError)
	email := fs.String("email", "", "the account the token belongs to, when not the acli sign-in's")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	token, err := readToken(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	if err := client.Login(ctx, *email, token); err != nil {
		return err
	}
	fmt.Println("The Teams token works and is saved in the OS secure store.")
	return nil
}

// readToken takes the token from a terminal, hidden as it is typed or
// pasted, or from a pipe.
func readToken(in *os.File, prompt io.Writer) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprint(prompt, "Paste the Teams API token (hidden): ")
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(prompt)
		return strings.TrimSpace(string(b)), err
	}
	return readTokenFrom(in)
}

func readTokenFrom(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, 1<<16))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", errors.New("no token on standard input")
	}
	return token, nil
}
