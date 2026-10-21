// Package actions runs the side effects triggered from the UI.
package actions

import (
	"os"
	"os/exec"
	"path/filepath"
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

// OpenFile opens a file or a folder with the system's default app.
func OpenFile(path string) error { return OpenBrowser(path) }

// Reveal shows a file in the system's file manager, selected where the
// manager can; a folder is just opened.
func Reveal(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return OpenFile(path)
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,"+path).Start()
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	}
	return OpenFile(filepath.Dir(path))
}
