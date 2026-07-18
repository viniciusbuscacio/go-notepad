package main

import (
	_ "embed"
)

// The embedded install wizard. The release ships this same binary twice:
// "go-notepad.exe" (in the zip) always runs the app, portable; the
// "-setup.exe" asset opens as the classic install wizard, and — run again
// with the app already installed — as the Reinstall/Uninstall maintenance
// screen. Mechanics live in the shared
// github.com/viniciusbuscacio/go-installer library; this app draws the
// wizard (InstallerView.vue). Real implementation in install_windows.go —
// the wizard is Windows-only; macOS ships a DMG and Linux has its CLI
// install (-install, see desktop_linux.go).

// projectURL is the app's public home: the license screen links it and the
// Apps & Features entry lists it.
const projectURL = "https://github.com/viniciusbuscacio/go-notepad"

// licenseText is the MIT license shown on the wizard's license screen.
//
//go:embed LICENSE
var licenseText string

// InstallerState is everything the wizard needs to draw itself. Mode ""
// means a normal app run (not the setup exe, or a non-Windows OS) and
// keeps the wizard hidden.
type InstallerState struct {
	Mode    string `json:"mode"` // "" | "wizard" | "maintenance" | "uninstall"
	Dir     string `json:"dir"`  // destination folder (default, user-chosen, or the existing install)
	Version string `json:"version"`
	// InstalledVersion is the already-installed version, for the
	// maintenance screen (setup run while the app is installed).
	InstalledVersion string `json:"installedVersion"`
	URL              string `json:"url"`
	License          string `json:"license"`
}
