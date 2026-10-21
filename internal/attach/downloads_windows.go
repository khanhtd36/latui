//go:build windows

package attach

import "golang.org/x/sys/windows"

// downloadsDir asks Windows where the Downloads folder is: users can move it.
func downloadsDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Downloads, windows.KF_FLAG_DEFAULT)
}
