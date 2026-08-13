package main

import (
	"fmt"
	"os"

	"github.com/bestruirui/octopus/cmd"
	"github.com/bestruirui/octopus/internal/update"
)

func main() {
	handled, err := update.RunWindowsUpdateHelper(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if handled {
		return
	}
	if update.WindowsUpdateCleanupPending() {
		go func() {
			if err := update.CleanupWindowsUpdate(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()
	}
	cmd.Execute()
}
