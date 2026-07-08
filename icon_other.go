//go:build !windows

package main

// fixTaskbarIcon is a no-op outside Windows; other platforms derive the taskbar
// icon from the bundle/desktop file.
func fixTaskbarIcon(string) {}
