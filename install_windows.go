//go:build windows

package main

import (
	"path/filepath"
	"strings"

	installer "github.com/viniciusbuscacio/go-installer/windows"
	"github.com/viniciusbuscacio/go-notepad/internal/settings"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// notepadInstaller describes this app to the go-installer library. dir is the
// custom destination ("" = the default %LOCALAPPDATA%\Programs\go-notepad).
func notepadInstaller(dir string) installer.App {
	var data []string
	if cfgDir, err := settings.ConfigDir(); err == nil {
		data = append(data, cfgDir)
	}
	return installer.App{
		ID:          "go-notepad",
		DisplayName: "go-Notepad",
		Version:     strings.TrimPrefix(appVersion, "v"),
		Publisher:   "Vinicius Buscacio",
		URL:         projectURL,
		Dir:         dir,
		DataDirs:    data,
	}
}

// installerCleanup runs the %TEMP% helper's removal pass when this process
// is the uninstall helper copy; true means exit before any UI starts.
func installerCleanup() bool { return installer.MaybeCleanup() }

// installerBoot decides what this launch is, before the window exists:
// the Apps & Features uninstall, a normal run (installed copy, or a plain
// non-setup exe — always portable), the wizard, or — setup run with the
// app already installed — the Reinstall/Uninstall maintenance screen,
// whose dir is the existing install so Reinstall lands in the same place.
func installerBoot() (mode, dir string) {
	if installer.UninstallRequested() {
		return "uninstall", ""
	}
	app := notepadInstaller("")
	if app.Installed() || !installer.RunningAsSetup() {
		return "", ""
	}
	if loc, _, ok := app.InstalledInfo(); ok {
		return "maintenance", loc
	}
	return "wizard", ""
}

func (a *App) InstallerState() InstallerState {
	a.mu.Lock()
	mode, dir := a.instMode, a.instDir
	a.mu.Unlock()
	if dir == "" {
		if d, err := notepadInstaller("").InstallDir(); err == nil {
			dir = d
		}
	}
	instVer := ""
	if mode == "maintenance" {
		_, instVer, _ = notepadInstaller("").InstalledInfo()
	}
	return InstallerState{
		Mode:             mode,
		Dir:              dir,
		Version:          strings.TrimPrefix(appVersion, "v"),
		InstalledVersion: instVer,
		URL:              projectURL,
		License:          licenseText,
	}
}

// InstallerChooseDir opens the folder picker for the destination screen and
// returns the new destination ("" = kept as is). The app installs into a
// "go-notepad" subfolder of whatever the user picks: uninstall deletes the
// whole install directory, so it must never share a folder the user owns.
func (a *App) InstallerChooseDir() string {
	picked, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Choose the install folder",
	})
	if err != nil || picked == "" {
		return ""
	}
	if !strings.EqualFold(filepath.Base(picked), "go-notepad") {
		picked = filepath.Join(picked, "go-notepad")
	}
	a.mu.Lock()
	a.instDir = picked
	a.mu.Unlock()
	return picked
}

// InstallerInstall copies the exe and registers the app in Apps & Features.
// Returns "" on success or the error message for the wizard to show.
func (a *App) InstallerInstall() string {
	a.mu.Lock()
	dir := a.instDir
	a.mu.Unlock()
	exe, err := notepadInstaller(dir).Install()
	if err != nil {
		return err.Error()
	}
	a.mu.Lock()
	a.instExe = exe
	a.mu.Unlock()
	return ""
}

// InstallerFinish creates the shortcuts picked on the final screen, opens
// the installed app if asked, and quits this (downloaded) copy.
func (a *App) InstallerFinish(startMenu, desktop, launch bool) string {
	a.mu.Lock()
	dir, exe := a.instDir, a.instExe
	a.mu.Unlock()
	if exe == "" {
		return "install has not run"
	}
	sc := installer.Shortcuts{StartMenu: startMenu, Desktop: desktop}
	if err := notepadInstaller(dir).CreateShortcuts(exe, sc); err != nil {
		return err.Error()
	}
	if launch {
		if err := installer.Launch(exe); err != nil {
			return err.Error()
		}
	}
	wruntime.Quit(a.ctx)
	return ""
}

// InstallerUninstall runs the full removal (shortcuts, registry, data, the
// install folder) and quits; the %TEMP% helper finishes after we exit.
func (a *App) InstallerUninstall() string {
	if err := notepadInstaller("").Uninstall(); err != nil {
		return err.Error()
	}
	wruntime.Quit(a.ctx)
	return ""
}
