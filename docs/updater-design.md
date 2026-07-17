# In-app updater — design

Status: implemented (17/jul/2026). Decisions below were settled with Vinicius
on 17/jul/2026. Deviations from the original draft, made during implementation:

- The binary swap is hand-rolled (rename dance with rollback) instead of
  pulling in `minio/selfupdate`, and the swap is covered by unit tests.
- The mechanics originally lived in `internal/updater` and were later
  extracted to the shared family library
  [go-updates](https://github.com/viniciusbuscacio/go-updates) (v0.2.1 of
  this app). This adapter (`update.go`) and the UI stay app-local by design.
- The settings fields are flat (`updateAutoCheck`, `updateSkippedVersion`,
  `updateLaterUntil`, `updateLastAutoCheck`), matching the existing flat
  `settings.json`, not a nested `updates` object.
- The stub-server scenarios (fake release, checksum mismatch never swaps) live
  in the `internal/updater` unit tests. `tools/smoke` checks the live surface:
  `/v1/update` contract, pressing `update-check`, and that the check settles
  into a structured outcome; set `GO_NOTEPAD_UPDATE_URL` when launching the
  app to point the updater at a stub during a smoke run.

## What it does

go-Notepad checks GitHub Releases for a newer version, tells the user, and —
if they click Install — downloads the new binary, verifies it, swaps itself
and restarts. No helper executable, no external installer.

## Decisions

| # | Topic | Decision |
|---|-------|----------|
| 1 | Trigger | Manual button **and** automatic check on startup |
| 2 | Auto-check frequency | At most once per day |
| 3 | Auto-check default | **Off** (the app never calls the network unasked; user opts in) |
| 4 | Dev builds | No special-casing; `appVersion=dev` compares as 0.0.0, so any release counts as newer |
| 5 | Channels | Latest stable release only (`/releases/latest`); pre-releases ignored |
| 6 | Where the notice lives | Dot badge on the title-bar gear + an **Updates** block in Settings (decided by Claude, see UX) |
| 7 | Release notes | Shown in-app, rendered as plain text from the GitHub release body (decided by Claude) |
| 8 | Skip this version | Skips that tag only; a newer tag notifies again |
| 9 | Later | 7-day cooldown before the badge/notice returns |
| 10 | Language | English, like the rest of the UI |
| 11 | Install | True self-update: download, verify, swap binary in place |
| 12 | Restart | Automatic: clicking Install closes the app as soon as the swap is done and relaunches the new binary |
| 13 | Download timing | Only after the user clicks Install (no speculative download) |
| 14 | Integrity | `checksums.txt` published with every release; app verifies SHA-256 before swapping (decided by Claude) |
| 15 | On failure | Structured error message; the current binary stays untouched and keeps running |
| 16 | Agent API | Exposed in `/v1/ax` and the REST API (risk-annotated, see below) |
| 17 | Smoke test | Covered via testids (free — smoke already walks every testid) + a check against a local stub server (decided by Claude) |
| 18 | Settings | New fields in the existing `internal/settings` JSON (decided by Claude) |
| 19 | Family strategy | go-notepad first; port to go-calc by copying `internal/updater` (current family pattern) |
| 20 | Dry-run release | Ship as v0.2.0, then a tiny v0.2.1 to exercise the full update cycle for real |

## UX

- **Settings → Updates block** (new section in OptionsView, above About):
  - "Check for updates automatically (once a day)" toggle — default off.
  - "Check now" button → spinner → result: "You're up to date (0.2.0)" or the
    update card.
  - Update card: new version number, plain-text release notes (scrollable,
    few lines tall), and three buttons: **Install and restart**,
    **Skip this version**, **Later**.
- **Badge**: when a check (manual or auto) finds an update that isn't skipped
  or inside the Later cooldown, the title-bar gear gets a small dot. Clicking
  the gear opens Settings as usual; the Updates block is highlighted. No toast,
  no interruption — the editor is the product, the badge is enough.
- **Install flow**: click Install → button becomes progress ("Downloading…",
  "Verifying…", "Restarting…") → session is flushed (same path as quit) → app
  swaps the binary, relaunches, exits. Total UI is one button changing state.
- **Failure**: card shows the structured error ("Could not replace the
  executable: permission denied…"), app keeps running on the old version.
  No browser fallback.

## Technical design

### `internal/updater` (pure Go, no Wails imports, testable)

- `Check(current string) (*Release, error)` — GET
  `https://api.github.com/repos/viniciusbuscacio/go-notepad/releases/latest`
  (unauthenticated; 60 req/h limit is plenty). Parses tag (`vX.Y.Z`), compares
  semver against `current` (`dev` → 0.0.0). Returns version, notes (release
  body), and the asset for `runtime.GOOS/GOARCH`
  (`go-notepad-vX.Y.Z-<os>-<arch>.{zip,tar.gz}`). No asset for the platform →
  structured error.
- `Install(rel *Release) error` — download asset to a temp file next to the
  executable, verify SHA-256 against `checksums.txt` from the same release,
  extract the binary (zip/tar.gz), then swap:
  - **Windows**: rename running `go-notepad.exe` → `go-notepad.exe.old`,
    move new one in place, spawn it, exit. `.old` removed on next startup.
  - **Linux**: replace the file at `os.Executable()` (rename over), spawn, exit.
  - **macOS**: replace the binary inside `go-Notepad.app/Contents/MacOS/`
    (bundle path derived from `os.Executable()`), spawn, exit. Binaries the
    app downloads itself carry no quarantine attribute, so Gatekeeper does
    not re-block the updated app.
  - Swap implemented with `minio/selfupdate` (small, battle-tested, does the
    Windows rename dance) — only new dependency.
- Base URL injectable (`UpdaterBaseURL`) so tests and smoke hit a local stub
  instead of GitHub.

### `internal/settings` — new fields

```json
{
  "updates": {
    "autoCheck": false,
    "skippedVersion": "",
    "laterUntil": "2026-07-24T00:00:00Z",
    "lastAutoCheck": "2026-07-17T00:00:00Z"
  }
}
```

`lastAutoCheck` enforces the once-a-day throttle; `laterUntil` the 7-day
cooldown; `skippedVersion` holds one tag.

### `app.go` adapter + frontend

- Bindings: `CheckForUpdates()`, `InstallUpdate()`, `SkipVersion(tag)`,
  `RemindLater()` — thin wrappers; state flows to the store
  (`update.available`, `update.version`, `update.notes`, `update.progress`).
- On startup: if `autoCheck` && last check > 24h → background check; result
  only flips the badge, never opens UI.
- Quit path reuses `flushSession()` before the swap so no keystrokes are lost.

### Agent API (`/v1/ax` + REST)

New controls, following the hardened contract (structured errors, risk field):

- `update-check` → `risk: external` (network call).
- `update-install` → `risk: destructive` (terminates the process).
- `update-skip`, `update-later` → `risk: safe`.
- New endpoint `GET /v1/update` returns the last check result
  (`{available, version, notes, skipped, laterUntil}`).

### `release.yml` change

Add a final job step (or a small aggregate job) that computes SHA-256 for the
three assets and uploads `checksums.txt` to the same release. The app refuses
to install when the checksum file is missing or does not match.

### Smoke test (`tools/smoke`)

- The new testids (updates toggle, check button, install/skip/later) are
  covered by the existing "every testid in /v1/ax is reachable" sweep.
- One added scenario: point `UpdaterBaseURL` at a local stub that serves a
  fake newer release + checksums, call `update-check` via the API, assert the
  structured response; assert `update-install` against the stub fails cleanly
  on checksum mismatch (never swaps). No real GitHub calls in CI.

## Rollout

1. Implement in go-notepad, gate green (unit tests on `internal/updater` with
   stub server + smoke).
2. Ship v0.2.0 (updater present, pointing at real releases).
3. Cut a tiny v0.2.1 (changelog tweak) and run the real update cycle on
   Windows/macOS/Linux as the acceptance test.
4. Port `internal/updater` + Updates UI to go-calc (repo/asset names differ
   only), same dry-run there.
