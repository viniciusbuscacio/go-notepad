//go:build windows

package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	shell32                      = syscall.NewLazyDLL("shell32.dll")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procExtractIconW             = shell32.NewProc("ExtractIconW")
	procGetModuleH               = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1
)

// findOwnWindow returns the first visible top-level window owned by this
// process. Matching by PID (instead of FindWindow on the title, which any
// other app could share) guarantees we never touch someone else's window.
func findOwnWindow() uintptr {
	var found uintptr
	pid := uint32(os.Getpid())
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1 // not ours; keep enumerating
		}
		if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
			return 1 // hidden helper window; keep looking
		}
		found = hwnd
		return 0 // stop enumerating
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// fixTaskbarIcon sets the window's *large* icon so the Windows taskbar shows
// the app icon. Wails only sets the small icon on frameless windows, which
// leaves ICON_BIG empty and the taskbar button blank on some Windows builds.
// It polls for the window (created shortly after startup), extracts the icon
// embedded in our own exe, and applies it.
func fixTaskbarIcon(_ string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exePtr, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return
	}
	hInst, _, _ := procGetModuleH.Call(0)

	for i := 0; i < 50; i++ { // up to ~5s
		if hwnd := findOwnWindow(); hwnd != 0 {
			hIcon, _, _ := procExtractIconW.Call(hInst, uintptr(unsafe.Pointer(exePtr)), 0)
			// ExtractIcon returns 0 (no icon) or 1 (bad index) on failure.
			if hIcon != 0 && hIcon != 1 {
				procSendMessageW.Call(hwnd, wmSetIcon, iconBig, hIcon)
				procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, hIcon)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
