//go:build windows

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const windowsUpdateHelperEnv = "OCTOPUS_UPDATE_HELPER"

const windowsUpdateCleanupEnv = "OCTOPUS_UPDATE_CLEANUP_DIR"

func activateExecutable(stagedPath, execPath, backupPath string) error {
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous rollback executable: %w", err)
	}
	if err := copyFile(execPath, backupPath); err != nil {
		return fmt.Errorf("preserve current executable: %w", err)
	}
	from, err := windows.UTF16PtrFromString(stagedPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(execPath)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("activate staged executable: %w", err)
	}
	return nil
}

func restartWithUpdate(execPath, stagedPath, backupPath, stagingDir string) error {
	helperPath := filepath.Join(stagingDir, "octopus-update-helper.exe")
	currentExecutable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable for update helper: %w", err)
	}
	if err := copyFile(currentExecutable, helperPath); err != nil {
		return fmt.Errorf("prepare update helper: %w", err)
	}
	command := exec.Command(helperPath,
		"__update-helper",
		strconv.Itoa(os.Getpid()),
		execPath,
		stagedPath,
		backupPath,
		stagingDir,
		"--",
	)
	command.Args = append(command.Args, os.Args[1:]...)
	command.Env = append(os.Environ(), windowsUpdateHelperEnv+"=1")
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("start update helper: %w", err)
	}
	return nil
}

func RunWindowsUpdateHelper(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "__update-helper" {
		return false, nil
	}
	if os.Getenv(windowsUpdateHelperEnv) != "1" {
		return true, fmt.Errorf("update helper authorization is missing")
	}
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator != 6 || len(args) < 7 {
		return true, fmt.Errorf("invalid update helper arguments")
	}
	pid, err := strconv.Atoi(args[1])
	if err != nil || pid <= 0 {
		return true, fmt.Errorf("invalid parent process ID %q", args[1])
	}
	execPath, stagedPath, backupPath, stagingDir := args[2], args[3], args[4], args[5]
	if err := validateWindowsUpdatePaths(execPath, stagedPath, backupPath, stagingDir); err != nil {
		return true, err
	}
	if err := waitForProcessExit(uint32(pid)); err != nil {
		return true, err
	}
	originalArgs := args[separator+1:]
	if err := activateExecutable(stagedPath, execPath, backupPath); err != nil {
		if restartErr := startWindowsExecutable(execPath, originalArgs, stagingDir); restartErr != nil {
			return true, fmt.Errorf("activate update: %w; restart current executable: %v", err, restartErr)
		}
		return true, nil
	}
	if err := startWindowsExecutable(execPath, originalArgs, stagingDir); err != nil {
		if rollbackErr := restoreWindowsExecutable(backupPath, execPath); rollbackErr != nil {
			return true, fmt.Errorf("start updated executable: %w; restore rollback executable: %v", err, rollbackErr)
		}
		if restartErr := startWindowsExecutable(execPath, originalArgs, stagingDir); restartErr != nil {
			return true, fmt.Errorf("start updated executable: %w; restart rollback executable: %v", err, restartErr)
		}
		return true, nil
	}
	return true, nil
}

func CleanupWindowsUpdate() error {
	stagingDir := strings.TrimSpace(os.Getenv(windowsUpdateCleanupEnv))
	if stagingDir == "" {
		return nil
	}
	_ = os.Unsetenv(windowsUpdateCleanupEnv)
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path for update cleanup: %w", err)
	}
	absExec, err := filepath.Abs(execPath)
	if err != nil {
		return err
	}
	absStaging, err := filepath.Abs(stagingDir)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Dir(absExec), filepath.Dir(absStaging)) ||
		!strings.HasPrefix(strings.ToLower(filepath.Base(absStaging)), ".octopus-update-") {
		return fmt.Errorf("invalid update cleanup directory")
	}

	var cleanupErr error
	for attempt := 0; attempt < 50; attempt++ {
		cleanupErr = os.RemoveAll(absStaging)
		if cleanupErr == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("remove update cleanup directory: %w", cleanupErr)
}

func WindowsUpdateCleanupPending() bool {
	return strings.TrimSpace(os.Getenv(windowsUpdateCleanupEnv)) != ""
}

func startWindowsExecutable(execPath string, args []string, stagingDir string) error {
	command := exec.Command(execPath, args...)
	command.Env = append(environmentWithout(windowsUpdateHelperEnv, windowsUpdateCleanupEnv), windowsUpdateCleanupEnv+"="+stagingDir)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Start()
}

func restoreWindowsExecutable(backupPath, execPath string) error {
	from, err := windows.UTF16PtrFromString(backupPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(execPath)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return err
	}
	return nil
}

func environmentWithout(keys ...string) []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		remove := false
		for _, key := range keys {
			if strings.EqualFold(name, key) {
				remove = true
				break
			}
		}
		if !remove {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func validateWindowsUpdatePaths(execPath, stagedPath, backupPath, stagingDir string) error {
	helperPath, err := os.Executable()
	if err != nil {
		return err
	}
	absHelper, err := filepath.Abs(helperPath)
	if err != nil {
		return err
	}
	absExec, err := filepath.Abs(execPath)
	if err != nil {
		return err
	}
	absStaged, err := filepath.Abs(stagedPath)
	if err != nil {
		return err
	}
	absBackup, err := filepath.Abs(backupPath)
	if err != nil {
		return err
	}
	absStaging, err := filepath.Abs(stagingDir)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Dir(absHelper), absStaging) ||
		!strings.EqualFold(filepath.Base(absHelper), "octopus-update-helper.exe") ||
		!strings.EqualFold(filepath.Dir(absExec), filepath.Dir(absStaging)) ||
		!strings.EqualFold(filepath.Dir(absStaged), absStaging) ||
		!strings.EqualFold(absBackup, absExec+".rollback") ||
		!strings.HasPrefix(strings.ToLower(filepath.Base(absStaging)), ".octopus-update-") {
		return fmt.Errorf("invalid update helper paths")
	}
	return nil
}

func waitForProcessExit(pid uint32) error {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return fmt.Errorf("open parent process: %w", err)
	}
	defer windows.CloseHandle(process)
	status, err := windows.WaitForSingleObject(process, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("wait for parent process: %w", err)
	}
	if status != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected parent wait status %#x", status)
	}
	return nil
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err := output.ReadFrom(input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}
