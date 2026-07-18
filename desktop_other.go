//go:build !linux

package main

import "runtime"

// installApp is a no-op off Linux: the build already produces a self-contained
// bundle/executable (a .app on macOS, a packaged .exe on Windows), so there is
// nothing to register with the desktop environment.
func installApp(uninstall bool) error {
	if uninstall {
		println("Nothing to uninstall on " + runtime.GOOS + ": go-Notepad is a self-contained app here.")
		return nil
	}
	println("Nothing to install on " + runtime.GOOS + ": go-Notepad is a self-contained app here.")
	return nil
}
