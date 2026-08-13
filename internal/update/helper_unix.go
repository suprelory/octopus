//go:build !windows

package update

func RunWindowsUpdateHelper(_ []string) (bool, error) {
	return false, nil
}

func CleanupWindowsUpdate() error {
	return nil
}

func WindowsUpdateCleanupPending() bool {
	return false
}
