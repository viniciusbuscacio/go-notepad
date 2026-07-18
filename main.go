package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

const appTitle = "go-Notepad"

func main() {
	// When this process is the uninstaller's %TEMP% helper copy, do the
	// removal and exit before any UI machinery starts.
	if installerCleanup() {
		return
	}

	app := NewApp()
	app.instMode, app.instDir = installerBoot()

	// Command-line actions (install/uninstall/help, file to open) run without
	// the GUI — but only on a normal launch: in installer modes the argv is
	// the wizard's own (Apps & Features passes --uninstall).
	if app.instMode == "" && handleCLI(os.Args[1:]) {
		return
	}

	// The editor is a roomy resizable window; the wizard is a fixed landscape one.
	width, height, minWidth, minHeight := 760, 560, 480, 420
	title := appTitle
	if app.instMode != "" {
		width, height, minWidth, minHeight = 600, 500, 600, 500
		title = appTitle + " Setup"
	}

	err := wails.Run(&options.App{
		Title:     title,
		Width:     width,
		Height:    height,
		MinWidth:  minWidth,
		MinHeight: minHeight, // editor: fits the tab strip, editor and status bar comfortably
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Frameless:        true,
		StartHidden:      true, // shown by the frontend once theme/opacity are applied (avoids startup flicker on Linux)
		BackgroundColour: &options.RGBA{R: 20, G: 24, B: 30, A: 0},
		OnStartup:        app.startup,
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windows.None,
		},
		Mac: &mac.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
		},
		Linux: &linux.Options{
			WindowIsTranslucent: true,
			WebviewGpuPolicy:    linux.WebviewGpuPolicyOnDemand,
			Icon:                appIcon,
			// ProgramName sets the window's app_id/WMClass. GNOME (esp. on
			// Wayland) matches this to a <ProgramName>.desktop file to show the
			// dock/taskbar icon. Keep it in sync with go-notepad.desktop.
			ProgramName: "go-notepad",
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
