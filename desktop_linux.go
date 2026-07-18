//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// desktopTemplate is the .desktop launcher, embedded so the binary can register
// itself with no external files. __EXEC__ / __ICON__ are filled in at install
// time. StartupWMClass in it must match linux.Options.ProgramName in main.go so
// GNOME/Wayland maps the running window to this launcher and shows its icon.
//
//go:embed build/linux/go-notepad.desktop
var desktopTemplate string

// appID matches ProgramName / StartupWMClass; it names the installed files.
const appID = "go-notepad"

// installApp copies the running binary, the embedded icon and a generated
// .desktop launcher into the per-user XDG locations (~/.local), so the icon
// shows up in the dock/taskbar and app grid. On Linux — especially
// GNOME/Wayland — the window's app_id is matched to a .desktop file rather than
// a runtime window icon, so this step is what makes the logo appear. No root
// needed. With uninstall=true it removes those same files.
func installApp(uninstall bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var (
		binDst     = filepath.Join(home, ".local", "bin", appID)
		iconDst    = filepath.Join(home, ".local", "share", "icons", "hicolor", "512x512", "apps", appID+".png")
		desktopDst = filepath.Join(home, ".local", "share", "applications", appID+".desktop")
	)

	if uninstall {
		for _, p := range []string{binDst, iconDst, desktopDst} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", p, err)
			}
		}
		refreshCaches(home)
		fmt.Println("go-Notepad uninstalled from ~/.local")
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}

	// Copy the binary unless we are already running from the install target.
	if self != binDst {
		if err := copyFile(self, binDst, 0o755); err != nil {
			return fmt.Errorf("install binary: %w", err)
		}
	}
	if err := writeFile(iconDst, appIcon, 0o644); err != nil {
		return fmt.Errorf("install icon: %w", err)
	}

	// Exec points at the copied binary; Icon is the bare theme name (go-notepad)
	// that the icon we just placed under hicolor resolves to.
	desktop := strings.NewReplacer("__EXEC__", binDst, "__ICON__", appID).Replace(desktopTemplate)
	if err := writeFile(desktopDst, []byte(desktop), 0o644); err != nil {
		return fmt.Errorf("write .desktop: %w", err)
	}

	refreshCaches(home)

	fmt.Println("Installed go-Notepad:")
	fmt.Println("  binary  :", binDst)
	fmt.Println("  icon    :", iconDst)
	fmt.Println("  launcher:", desktopDst)
	fmt.Println()
	fmt.Println("Launch it from your app grid (search 'go-Notepad') so the dock icon binds")
	fmt.Println("to the .desktop file. If ~/.local/bin isn't on PATH, add it to run 'go-notepad'.")
	return nil
}

// refreshCaches nudges the desktop database and icon cache so the launcher and
// icon appear (or disappear) without a re-login. Both tools are optional; a
// missing one is not an error.
func refreshCaches(home string) {
	tryRun("update-desktop-database", filepath.Join(home, ".local", "share", "applications"))
	tryRun("gtk-update-icon-cache", "-f", "-t", filepath.Join(home, ".local", "share", "icons", "hicolor"))
}

func tryRun(name string, args ...string) {
	if _, err := exec.LookPath(name); err != nil {
		return
	}
	_ = exec.Command(name, args...).Run()
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeFile(dst, data, mode)
}

func writeFile(dst string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}
	// WriteFile respects umask, so set the mode explicitly (the binary needs +x).
	return os.Chmod(dst, mode)
}
