// Package actions runs the side effects triggered from the UI.
package actions

import (
	"os/exec"
	"runtime"

	"github.com/atotto/clipboard"
)

func OpenBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

func CopyToClipboard(s string) error { return clipboard.WriteAll(s) }
