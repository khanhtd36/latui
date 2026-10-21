//go:build !windows

package attach

import (
	"os"
	"path/filepath"
)

// downloadsDir is ~/Downloads.
func downloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}
