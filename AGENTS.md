# go-notepad — agent notes

Tabbed notepad of the [go-apps](https://github.com/viniciusbuscacio/go-apps)
family — and the family's **visual reference**: when another app's UI is in
doubt, it should look like go-notepad does.

**Before changing anything, read the family rules** — engineering:
[go-apps/AGENTS.md](https://github.com/viniciusbuscacio/go-apps/blob/main/AGENTS.md)
(local sibling checkout: `../go-apps/AGENTS.md`) — UI/visuals:
[go-design](https://github.com/viniciusbuscacio/go-design)
(`../go-design/README.md`).

App specifics:

- Text engine in `internal/notes` (pure Go); `app.go` is the Wails adapter.
- REST API port: family-shared range **8000–8999**, random default per
  install; domain endpoint `POST /v1/stats`.
- Editor-only zoom (Ctrl +/−/0, Ctrl+wheel) via `--editor-font-size`;
  bundled editor fonts (.woff2, OFL). Tabs default to the left.
- Updater design doc (family reference): `docs/updater-design.md`.
- Smoke suite: `go run ./tools/smoke` with the app open and the server on.
- Gate before commit: `go vet ./...`, `go test ./...`, `wails build`, smoke.
