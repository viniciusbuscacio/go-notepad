package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pendingFilePath is the absolute path of a file passed on the command line
// (go-notepad file.txt). It is set before the GUI starts and handed to the
// frontend once via ConsumePendingFile, which opens it in a tab.
var pendingFilePath string

// handleCLI intercepts the small set of command-line actions the binary
// supports before the GUI starts. It returns true when it handled a command
// and main() should exit without launching the window.
//
// The point is desktop integration without a separate installer or any shell
// script: the same binary that runs the app can also register itself with the
// desktop environment (Linux) — "everything in Go", end to end. On macOS and
// Windows the build already produces a self-contained bundle/exe, so these are
// friendly no-ops.
//
// Only the first argument is examined, and flags must match exactly: a typo
// like -instal prints the usage and exits non-zero instead of silently opening
// the GUI. A first argument that is not a flag is a file to open in the editor.
func handleCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "-install":
		exit(installApp(false))
		return true
	case "-uninstall":
		exit(installApp(true))
		return true
	case "-h", "-help":
		printUsage(os.Stdout)
		return true
	}
	// macOS may still pass a legacy -psn_... process serial number when the
	// bundle is double-clicked; it is not a user flag, so don't fail on it.
	if strings.HasPrefix(args[0], "-psn_") {
		return false
	}
	if strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "unknown flag: %s\n\n", args[0])
		printUsage(os.Stderr)
		os.Exit(2)
	}
	setPendingFile(args[0])
	return false
}

// setPendingFile validates the file argument and records it for the frontend.
// A path that does not exist yet is fine — the editor opens it as a new
// document that will be created on the first save — but an existing
// non-regular file (a directory, a device) is an error.
func setPendingFile(arg string) {
	p, err := filepath.Abs(arg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	if info, err := os.Stat(p); err == nil && !info.Mode().IsRegular() {
		fmt.Fprintf(os.Stderr, "error: not a regular file: %s\n", p)
		os.Exit(2)
	}
	pendingFilePath = p
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, `%s — a Windows 11-style tabbed notepad.

Usage:
  go-notepad              launch the app
  go-notepad FILE         launch the app and open FILE (created on first save if new)
  go-notepad -install     add the dock icon and app-grid entry (Linux; installs under ~/.local)
  go-notepad -uninstall   remove that desktop integration again
  go-notepad -help        show this message
`, appTitle)
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
