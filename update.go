package main

// The in-app updater's thin Wails adapter. All real work lives in
// internal/updater; this file owns the app-side state machine (last check
// result, notify rules, install progress) and pushes every change to the
// frontend as an "update:state" event, so the UI and GET /v1/update always
// agree. See docs/updater-design.md.

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/viniciusbuscacio/go-notepad/internal/apiserver"
	"github.com/viniciusbuscacio/go-notepad/internal/settings"
	"github.com/viniciusbuscacio/go-notepad/internal/updater"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	updateRepo = "viniciusbuscacio/go-notepad"
	// laterCooldown is how long "Later" silences the update notice.
	laterCooldown = 7 * 24 * time.Hour
	// autoCheckEvery throttles the automatic startup check.
	autoCheckEvery = 24 * time.Hour
)

// UpdateInfo is the single update snapshot shared by the frontend (binding +
// "update:state" event) and GET /v1/update.
type UpdateInfo struct {
	Checking   bool   `json:"checking"`
	Installing bool   `json:"installing"`
	Progress   string `json:"progress"` // downloading | verifying | applying, while installing
	Available  bool   `json:"available"`
	Version    string `json:"version"` // newest published version, once checked
	Notes      string `json:"notes"`   // its release notes (plain text)
	Current    string `json:"current"` // the running version
	CheckedAt  string `json:"checkedAt"`
	Error      string `json:"error"`
	// Notify is the badge rule, computed in Go so UI and API agree: an update
	// is available, the user didn't skip this tag, and "Later" has lapsed.
	Notify bool `json:"notify"`
}

// updateClient builds the updater for the real GitHub — or for a stub when
// GO_NOTEPAD_UPDATE_URL is set (tests / smoke runs).
func (a *App) updateClient() *updater.Client {
	return &updater.Client{
		BaseURL: os.Getenv("GO_NOTEPAD_UPDATE_URL"),
		Repo:    updateRepo,
		App:     "go-notepad",
	}
}

// setUpdState mutates the update snapshot under the lock and broadcasts the
// result to the frontend.
func (a *App) setUpdState(fn func(*UpdateInfo)) UpdateInfo {
	a.mu.Lock()
	fn(&a.updState)
	a.updState.Current = appVersion
	info := a.updState
	cfg := a.cfg
	a.mu.Unlock()
	info.Notify = computeNotify(info, cfg)
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "update:state", info)
	}
	return info
}

func computeNotify(info UpdateInfo, cfg settings.Settings) bool {
	if !info.Available || info.Installing {
		return false
	}
	if cfg.UpdateSkipped != "" && cfg.UpdateSkipped == "v"+info.Version {
		return false
	}
	if until, err := time.Parse(time.RFC3339, cfg.UpdateLaterUntil); err == nil && time.Now().Before(until) {
		return false
	}
	return true
}

// GetUpdateInfo returns the current snapshot without touching the network.
func (a *App) GetUpdateInfo() UpdateInfo {
	a.mu.Lock()
	info := a.updState
	info.Current = appVersion
	cfg := a.cfg
	a.mu.Unlock()
	info.Notify = computeNotify(info, cfg)
	return info
}

// CheckForUpdates asks GitHub for the latest release, right now. Backs the
// "Check for updates" button and the update-check API control.
func (a *App) CheckForUpdates() UpdateInfo {
	a.setUpdState(func(u *UpdateInfo) {
		u.Checking = true
		u.Error = ""
	})
	rel, newer, err := a.updateClient().Check(appVersion)
	now := time.Now().UTC().Format(time.RFC3339)
	// Any completed attempt counts for the once-a-day throttle, so a manual
	// check also resets the automatic one.
	_ = a.mutate(func(s *settings.Settings) { s.UpdateLastCheck = now })
	return a.setUpdState(func(u *UpdateInfo) {
		u.Checking = false
		u.CheckedAt = now
		if err != nil {
			u.Error = err.Error()
			return
		}
		u.Available = newer
		u.Version = rel.Version
		u.Notes = rel.Notes
		a.updRelease = rel
	})
}

// InstallUpdate downloads, verifies and applies the available update, then
// relaunches the new binary and closes this one. On failure the running app is
// untouched and keeps running; the error lands in the update state (and is
// returned) for the UI/API to show.
func (a *App) InstallUpdate() error {
	a.mu.Lock()
	rel := a.updRelease
	installing := a.updState.Installing
	a.mu.Unlock()
	if installing {
		return fmt.Errorf("an install is already in progress")
	}
	if rel == nil || !a.GetUpdateInfo().Available {
		return fmt.Errorf("no update available to install; check for updates first")
	}

	a.setUpdState(func(u *UpdateInfo) {
		u.Installing = true
		u.Error = ""
	})
	client := a.updateClient()
	err := client.Install(rel, func(stage string) {
		a.setUpdState(func(u *UpdateInfo) { u.Progress = stage })
	})
	if err != nil {
		a.setUpdState(func(u *UpdateInfo) {
			u.Installing = false
			u.Progress = ""
			u.Error = err.Error()
		})
		return err
	}
	// The executable on disk is now the new version: start it and leave. The
	// frontend has already flushed the session (same path as quitting).
	if err := client.Relaunch(); err != nil {
		a.setUpdState(func(u *UpdateInfo) {
			u.Installing = false
			u.Progress = ""
			u.Error = "updated, but the new version could not be started: " + err.Error() +
				"; it will be used the next time you open the app"
		})
		return err
	}
	wruntime.Quit(a.ctx)
	return nil
}

// SkipUpdateVersion silences the currently available version; a newer tag
// will notify again.
func (a *App) SkipUpdateVersion() error {
	info := a.GetUpdateInfo()
	if !info.Available {
		return fmt.Errorf("no update available to skip")
	}
	if err := a.mutate(func(s *settings.Settings) { s.UpdateSkipped = "v" + info.Version }); err != nil {
		return err
	}
	a.setUpdState(func(u *UpdateInfo) {}) // re-broadcast: Notify flips off
	return nil
}

// RemindUpdateLater snoozes the notice for 7 days.
func (a *App) RemindUpdateLater() error {
	until := time.Now().Add(laterCooldown).UTC().Format(time.RFC3339)
	if err := a.mutate(func(s *settings.Settings) { s.UpdateLaterUntil = until }); err != nil {
		return err
	}
	a.setUpdState(func(u *UpdateInfo) {})
	return nil
}

// SetUpdateAutoCheck toggles the once-a-day automatic check.
func (a *App) SetUpdateAutoCheck(v bool) error {
	return a.mutate(func(s *settings.Settings) { s.UpdateAutoCheck = v })
}

// maybeAutoCheck runs the startup check when the user opted in and the last
// one is old enough. Called from startup in a goroutine: the result only flips
// the badge via the update:state event, it never opens UI.
func (a *App) maybeAutoCheck() {
	cfg := a.snapshot()
	if !cfg.UpdateAutoCheck {
		return
	}
	if last, err := time.Parse(time.RFC3339, cfg.UpdateLastCheck); err == nil && time.Since(last) < autoCheckEvery {
		return
	}
	a.CheckForUpdates()
}

// handleUpdate serves GET /v1/update: the same snapshot the UI renders.
func (a *App) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiserver.WriteErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, a.GetUpdateInfo())
}
