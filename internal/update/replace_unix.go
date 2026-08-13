//go:build !windows

package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func activateExecutable(stagedPath, execPath, backupPath string) error {
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Remove(backupPath); err != nil {
			return fmt.Errorf("remove previous rollback executable: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect rollback executable: %w", err)
	}
	if err := copyExecutable(execPath, backupPath); err != nil {
		return fmt.Errorf("preserve current executable: %w", err)
	}
	if err := os.Rename(stagedPath, execPath); err != nil {
		return fmt.Errorf("activate staged executable: %w", err)
	}
	if err := syncDirectory(filepath.Dir(execPath)); err != nil {
		if rollbackErr := os.Rename(backupPath, execPath); rollbackErr != nil {
			return fmt.Errorf("sync installed executable: %w; restore rollback executable: %v", err, rollbackErr)
		}
		if rollbackSyncErr := syncDirectory(filepath.Dir(execPath)); rollbackSyncErr != nil {
			return fmt.Errorf("sync installed executable: %w; sync restored executable: %v", err, rollbackSyncErr)
		}
		return fmt.Errorf("sync installed executable: %w; current executable restored", err)
	}
	return nil
}

func restartWithUpdate(execPath, stagedPath, backupPath, stagingDir string) error {
	return restartInPlace(execPath, stagedPath, backupPath, stagingDir)
}

func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = output.Close()
		if !ok {
			_ = os.Remove(target)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	ok = true
	return syncDirectory(filepath.Dir(target))
}
