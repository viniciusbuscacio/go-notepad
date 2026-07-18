//go:build !windows

package main

// The install wizard is Windows-only (macOS ships a DMG, Linux has the CLI
// install in desktop_linux.go). These stubs keep the App bindings — and so
// the generated frontend API — identical on every OS; Mode "" keeps the
// wizard hidden.

func installerCleanup() bool { return false }

func installerBoot() (mode, dir string) { return "", "" }

func (a *App) InstallerState() InstallerState { return InstallerState{} }

func (a *App) InstallerChooseDir() string { return "" }

func (a *App) InstallerInstall() string { return "not supported on this OS" }

func (a *App) InstallerFinish(startMenu, desktop, launch bool) string {
	return "not supported on this OS"
}

func (a *App) InstallerUninstall() string { return "not supported on this OS" }
