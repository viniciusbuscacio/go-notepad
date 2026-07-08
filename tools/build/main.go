// Command build is the cross-platform build entry point for go-Notepad.
//
// One Go program replaces per-OS shell scripts: it runs the same way on
// Windows, Linux and macOS and produces a native binary for whichever OS it
// runs on. It is a thin, readable wrapper around the Wails CLI — the actual
// compilation (frontend + embedded assets + Go) is still `wails build`.
//
//	go run ./tools/build              # production build for this OS
//	go run ./tools/build -test        # run tests first, then build
//	go run ./tools/build -clean       # wipe build/bin before building
//	go run ./tools/build -- -nsis     # forward extra flags to `wails build`
//
// Note on cross-compiling: a Wails app embeds the host's native webview, so a
// Windows/Linux/macOS binary is built on that same OS. This tool gives every OS
// one identical command; CI runs it on three runners to produce all three.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	runTests := flag.Bool("test", false, "run `go test ./internal/...` before building")
	clean := flag.Bool("clean", false, "remove build/bin before building")
	flag.Parse()

	if err := build(*runTests, *clean, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "\nbuild failed:", err)
		os.Exit(1)
	}
}

func build(runTests, clean bool, extra []string) error {
	if err := chdirToRoot(); err != nil {
		return err
	}
	if _, err := exec.LookPath("wails"); err != nil {
		return fmt.Errorf("the Wails CLI is not in PATH; install it with:\n" +
			"  go install github.com/wailsapp/wails/v2/cmd/wails@latest")
	}

	if runTests {
		if err := sh("go", "test", "./internal/..."); err != nil {
			return err
		}
	}
	if clean {
		if err := os.RemoveAll(filepath.Join("build", "bin")); err != nil {
			return fmt.Errorf("clean: %w", err)
		}
	}

	// On Linux, modern distros (Ubuntu 22.04+/24.04, Zorin, Fedora, etc.) ship
	// only WebKitGTK 4.1; the 4.0 pkg-config is gone. Wails needs the
	// `webkit2_41` build tag to link against 4.1. We add it automatically so
	// `go run ./tools/build` works out of the box on every modern Linux.
	args := []string{"build"}
	if runtime.GOOS == "linux" {
		args = append(args, "-tags", "webkit2_41")
	}
	args = append(args, extra...)
	if err := sh("wails", args...); err != nil {
		return err
	}

	bin := "go-notepad"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out := filepath.Join("build", "bin", bin)
	if info, err := os.Stat(out); err == nil {
		fmt.Printf("\nBuilt %s (%.1f MB) for %s/%s\n",
			out, float64(info.Size())/(1<<20), runtime.GOOS, runtime.GOARCH)
	}
	return nil
}

// chdirToRoot walks up from the current directory until it finds wails.json, so
// the tool works no matter where it is launched from.
func chdirToRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "wails.json")); err == nil {
			return os.Chdir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("wails.json not found; run this from within the go-notepad repo")
		}
		dir = parent
	}
}

func sh(name string, args ...string) error {
	fmt.Println("$", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}
