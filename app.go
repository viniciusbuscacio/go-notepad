package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/viniciusbuscacio/go-notepad/internal/apiserver"
	"github.com/viniciusbuscacio/go-notepad/internal/notes"
	"github.com/viniciusbuscacio/go-notepad/internal/settings"
	"github.com/viniciusbuscacio/go-notepad/internal/updater"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// API port range the shuffle button picks from.
const (
	portRangeBase = 8800
	portRangeSpan = 100 // 8800..8899
)

// App is the thin Wails adapter. Business logic lives in internal/*; App just
// wires it to the frontend and owns process-level state (settings + server).
type App struct {
	ctx context.Context
	// mu guards cfg: bound methods (JS thread) and the HTTP handlers (via
	// status/apiURL) touch it concurrently. Anti-deadlock rule: lock, copy or
	// mutate, unlock — never hold mu across settings.Save or a server
	// start/stop (the server has its own lock).
	mu     sync.Mutex
	cfg    settings.Settings
	server *apiserver.Server
	ui     *uiBridge
	// In-app updater state (see update.go): the last check's snapshot and the
	// release it found, kept so Install doesn't need to re-check.
	updState   UpdateInfo
	updRelease *updater.Release
}

// snapshot returns a copy of the current settings; callers work on the copy so
// mu is never held while saving or (re)starting the server.
func (a *App) snapshot() settings.Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

func NewApp() *App {
	a := &App{}
	a.ui = newUIBridge(a)
	a.server = apiserver.New(a.textStats, a.appInfo, a.ui)
	a.server.HandleExtra("/v1/update", a.handleUpdate)
	return a
}

// textStats is the app-specific direct operation exposed at POST /v1/stats:
// given a block of text, return its line/word/character counts.
func (a *App) textStats(text string) (any, error) {
	return notes.Compute(text), nil
}

// UIAck is called by the frontend to report the resulting screen state after
// executing a ui:command. It is bound to JS by Wails.
func (a *App) UIAck(id string, state string) {
	a.ui.ack(id, json.RawMessage(state))
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	cfg := settings.Load()
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	go fixTaskbarIcon(appTitle)
	// Sweep the ".old" binary a previous self-update parked, then — if the
	// user opted in — check for a newer release in the background.
	a.updateClient().CleanupOld()
	go a.maybeAutoCheck()
	if cfg.APIAutoStart {
		if err := a.startServer(); err != nil {
			// No UI is up yet to show this, so at least leave a trace on stderr
			// instead of swallowing it; the API panel will show "Stopped".
			fmt.Fprintln(os.Stderr, "go-notepad: API server autostart failed:", err)
		}
	}
}

// Stats returns the line/word/character counts for text. The frontend calls it
// to fill the status bar — the same engine that backs POST /v1/stats — so the
// numbers on screen and over the API always agree (logic lives in Go).
func (a *App) Stats(text string) notes.Stats {
	return notes.Compute(text)
}

// ---- File operations ----

// FileResult is what a native open/save dialog hands back to the frontend: the
// chosen path, its base name (the tab title), and — for opens — the contents.
type FileResult struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Canceled bool   `json:"canceled"` // true when the user dismissed the dialog
}

var textFilters = []wruntime.FileFilter{
	{DisplayName: "Text files (*.txt, *.md, *.log)", Pattern: "*.txt;*.md;*.log"},
	{DisplayName: "All files (*.*)", Pattern: "*.*"},
}

// OpenFile shows a native open dialog and, if the user picks a file, reads it.
func (a *App) OpenFile() (FileResult, error) {
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:   "Open",
		Filters: textFilters,
	})
	if err != nil {
		return FileResult{}, err
	}
	if path == "" {
		return FileResult{Canceled: true}, nil
	}
	content, err := notes.Load(path)
	if err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: path, Name: filepath.Base(path), Content: content}, nil
}

// SaveFile writes content to an already-known path (Ctrl+S on a saved file).
func (a *App) SaveFile(path, content string) error {
	return notes.Save(path, content)
}

// OpenPath reads the file at path directly, with no dialog. It backs restoring
// a non-dirty session tab from what is actually on disk (the file may have
// been edited by another program since the session was written) and opening a
// file passed on the command line.
func (a *App) OpenPath(path string) (FileResult, error) {
	content, err := notes.Load(path)
	if err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: path, Name: filepath.Base(path), Content: content}, nil
}

// ConsumePendingFile returns the file path passed on the command line
// (go-notepad file.txt), or "" if there is none. It clears the path, so only
// the first caller — the frontend, right after restoring the session — gets it.
func (a *App) ConsumePendingFile() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := pendingFilePath
	pendingFilePath = ""
	return p
}

// SaveFileAs shows a native save dialog and writes content to the chosen path
// (Ctrl+Shift+S, or the first save of an untitled tab).
func (a *App) SaveFileAs(suggestedName, content string) (FileResult, error) {
	if suggestedName == "" {
		suggestedName = "Untitled.txt"
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "Save As",
		DefaultFilename: suggestedName,
		Filters:         textFilters,
	})
	if err != nil {
		return FileResult{}, err
	}
	if path == "" {
		return FileResult{Canceled: true}, nil
	}
	if err := notes.Save(path, content); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: path, Name: filepath.Base(path)}, nil
}

// ---- Session (reopen with the same tabs) ----
//
// The frontend serializes its open tabs (names, paths, contents) to JSON; we
// persist that next to settings.json in the per-user app-data directory — NOT
// the OS temp dir, which is wiped on reboot and would lose the session. On the
// next launch the frontend reads it back and rebuilds the tabs.

func sessionPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// SaveSession writes the serialized tab state to disk.
func (a *App) SaveSession(data string) error {
	p, err := sessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// Atomic write: a crash mid-write must never truncate the previous session,
	// which is the only copy of any unsaved tabs.
	return notes.WriteFileAtomic(p, []byte(data), 0o600)
}

// LoadSession returns the serialized tab state, or "" if there is none yet.
func (a *App) LoadSession() string {
	p, err := sessionPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---- Settings ----

func (a *App) GetSettings() settings.Settings {
	return a.snapshot()
}

// mutate applies fn to the settings under the lock, then persists the result
// (outside the lock). All the small setters below funnel through it.
func (a *App) mutate(fn func(*settings.Settings)) error {
	a.mu.Lock()
	fn(&a.cfg)
	cfg := a.cfg
	a.mu.Unlock()
	return settings.Save(cfg)
}

// SetTabPosition places the tab strip on top (default) or on the left, like
// vertical tabs in a browser.
func (a *App) SetTabPosition(pos string) error {
	if pos != "left" {
		pos = "top"
	}
	return a.mutate(func(s *settings.Settings) { s.TabPosition = pos })
}

// SetWordWrap toggles wrapping long lines in the editor.
func (a *App) SetWordWrap(v bool) error {
	return a.mutate(func(s *settings.Settings) { s.WordWrap = v })
}

// SetFontFamily sets the editor font (key: mono | sans | serif | courier).
func (a *App) SetFontFamily(family string) error {
	return a.mutate(func(s *settings.Settings) { s.FontFamily = family })
}

// SetFontSize sets the editor font size in px, clamped to the allowed range.
// It backs both the Settings control and the Ctrl +/- (and Ctrl+wheel) zoom.
func (a *App) SetFontSize(px int) error {
	if px < settings.FontSizeMin {
		px = settings.FontSizeMin
	}
	if px > settings.FontSizeMax {
		px = settings.FontSizeMax
	}
	return a.mutate(func(s *settings.Settings) { s.FontSize = px })
}

func (a *App) SetTheme(theme string) error {
	if theme != "light" {
		theme = "dark"
	}
	return a.mutate(func(s *settings.Settings) { s.Theme = theme })
}

func (a *App) SetOpacity(percent int) error {
	if percent < 20 {
		percent = 20
	}
	if percent > 100 {
		percent = 100
	}
	return a.mutate(func(s *settings.Settings) { s.Opacity = percent })
}

// ---- REST API server ----

// APIStatus is the snapshot the frontend renders.
type APIStatus struct {
	Running     bool   `json:"running"`
	Port        int    `json:"port"`
	URL         string `json:"url"`
	TLS         bool   `json:"tls"`
	Fingerprint string `json:"fingerprint"` // public-key pin, set while TLS is running
}

// apiURLFor builds the base URL for a settings snapshot. APIHTTPS is a direct
// user choice (the "Use HTTPS" toggle), independent of the bind address.
func apiURLFor(cfg settings.Settings) string {
	host := "127.0.0.1"
	if apiserver.BindHost(cfg.APIAllowlist) == "0.0.0.0" {
		host = apiserver.OutboundIP()
	}
	scheme := "http"
	if cfg.APIHTTPS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, cfg.APIPort)
}

func (a *App) apiURL() string {
	return apiURLFor(a.snapshot())
}

func (a *App) status() APIStatus {
	cfg := a.snapshot()
	return APIStatus{
		Running:     a.server.Running(),
		Port:        cfg.APIPort,
		URL:         apiURLFor(cfg),
		TLS:         cfg.APIHTTPS,
		Fingerprint: a.server.Fingerprint(),
	}
}

func (a *App) startServer() error {
	dir, err := settings.ConfigDir()
	if err != nil {
		return err
	}
	cfg := a.snapshot()
	return a.server.Start(apiserver.Config{
		Port:      cfg.APIPort,
		Key:       cfg.APIKey,
		Allowlist: cfg.APIAllowlist,
		TLS:       cfg.APIHTTPS,
		CertDir:   dir,
	})
}

// applyIfRunning restarts the server so config changes (key, allowlist) take
// effect immediately while it is running. The error is returned — not
// swallowed — so callers can surface a failed restart (which leaves the server
// stopped) to the UI.
func (a *App) applyIfRunning() error {
	if !a.server.Running() {
		return nil
	}
	if err := a.server.Stop(); err != nil {
		return err
	}
	return a.startServer()
}

func (a *App) StartAPIServer() (APIStatus, error) {
	if err := a.startServer(); err != nil {
		return a.status(), err
	}
	return a.status(), nil
}

func (a *App) StopAPIServer() (APIStatus, error) {
	if err := a.server.Stop(); err != nil {
		return a.status(), err
	}
	return a.status(), nil
}

func (a *App) GetAPIStatus() APIStatus {
	return a.status()
}

// ShuffleAPIPort picks a random FREE port in 8800–8899 (different from the
// current one), persists it, and restarts the server if running. It probes for
// a free port so pressing the button actually escapes an occupied port.
func (a *App) ShuffleAPIPort() (APIStatus, error) {
	// Probe for the new port outside the lock (it opens sockets), then commit.
	before := a.snapshot()
	port := pickFreePort(before.APIPort, apiserver.BindHost(before.APIAllowlist))
	if err := a.mutate(func(s *settings.Settings) { s.APIPort = port }); err != nil {
		return a.status(), err
	}
	if err := a.applyIfRunning(); err != nil {
		return a.status(), err
	}
	return a.status(), nil
}

// pickFreePort returns a random bindable port in the range, avoiding exclude.
// It falls back to any random port ≠ exclude if none probe as free.
func pickFreePort(exclude int, host string) int {
	for i := 0; i < 40; i++ {
		p := portRangeBase + rand.IntN(portRangeSpan)
		if p == exclude {
			continue
		}
		if ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, p)); err == nil {
			_ = ln.Close()
			return p
		}
	}
	next := exclude
	for next == exclude {
		next = portRangeBase + rand.IntN(portRangeSpan)
	}
	return next
}

func (a *App) SetAPIAutoStart(v bool) error {
	return a.mutate(func(s *settings.Settings) { s.APIAutoStart = v })
}

// SetHTTPS chooses the transport (HTTPS when true, plain HTTP when false), then
// restarts the server if running so the change (scheme + fingerprint) applies
// immediately.
func (a *App) SetHTTPS(v bool) (APIStatus, error) {
	if err := a.mutate(func(s *settings.Settings) { s.APIHTTPS = v }); err != nil {
		return a.status(), err
	}
	if err := a.applyIfRunning(); err != nil {
		return a.status(), err
	}
	return a.status(), nil
}

// GetAPIFingerprint returns the public-key pin while the TLS server is running
// (empty otherwise). A client pins it: curl --pinnedpubkey sha256//<fingerprint>.
func (a *App) GetAPIFingerprint() string {
	return a.server.Fingerprint()
}

func (a *App) GetAllowlist() []string {
	return a.snapshot().APIAllowlist
}

func (a *App) AddAllowlistEntry(entry string) ([]string, error) {
	normalized, err := apiserver.NormalizeCIDR(entry)
	if err != nil {
		return a.GetAllowlist(), err
	}
	var list []string
	if err := a.mutate(func(s *settings.Settings) {
		for _, e := range s.APIAllowlist {
			if e == normalized {
				list = s.APIAllowlist
				return
			}
		}
		s.APIAllowlist = append(s.APIAllowlist, normalized)
		list = s.APIAllowlist
	}); err != nil {
		return list, err
	}
	if err := a.applyIfRunning(); err != nil {
		return list, err
	}
	return list, nil
}

func (a *App) RemoveAllowlistEntry(entry string) ([]string, error) {
	// Refuse to empty the allowlist: with no allowed CIDR every client is
	// rejected — including this machine — so there would be no way back in
	// over the API. The last entry must be replaced, not removed.
	a.mu.Lock()
	cur := a.cfg.APIAllowlist
	next := make([]string, 0, len(cur))
	for _, e := range cur {
		if e != entry {
			next = append(next, e)
		}
	}
	if len(next) == 0 && len(cur) > 0 {
		a.mu.Unlock()
		return cur, fmt.Errorf("cannot remove the last allowlist entry")
	}
	a.cfg.APIAllowlist = next
	cfg := a.cfg
	a.mu.Unlock()
	if err := settings.Save(cfg); err != nil {
		return next, err
	}
	if err := a.applyIfRunning(); err != nil {
		return next, err
	}
	return next, nil
}

func (a *App) GetAPIKey() string {
	return a.snapshot().APIKey
}

func (a *App) RotateAPIKey() (string, error) {
	key := settings.GenerateKey()
	if err := a.mutate(func(s *settings.Settings) { s.APIKey = key }); err != nil {
		return key, err
	}
	if err := a.applyIfRunning(); err != nil {
		return key, err
	}
	return key, nil
}

func (a *App) GetAPIURL() string {
	return a.apiURL()
}

// GetVersion returns the app version so the frontend can show it (Settings →
// About). Single source of truth: the same appVersion reported in /v1/ax.
func (a *App) GetVersion() string {
	return appVersion
}
