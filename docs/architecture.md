# Architecture

go-Notepad is a [Wails](https://wails.io) app: a Go backend and a Vue 3 / TypeScript
frontend in one binary, with the frontend embedded via `//go:embed`. The whole
design follows one rule:

> **TypeScript only paints the screen; all logic lives in Go.**

The point of that rule is reuse. The interesting, reusable parts of this project
are not the notepad itself — they are the layering and the agent control plane. A
future file explorer or calculator keeps the same skeleton and swaps only the
engine and the views.

## Layers

```
┌────────────────────────────────────────────────────────────┐
│ frontend/  (Vue 3, TypeScript)                              │
│   dumb UI: renders state, forwards intent. No business rules.│
└───────────────┬────────────────────────────────────────────┘
                │  Wails bindings (generated) + events
┌───────────────▼────────────────────────────────────────────┐
│ app.go / appinfo.go / uibridge.go  (the adapter)            │
│   the ONLY code that imports Wails. Wires the core to the   │
│   window, exposes methods to JS, drives the live UI.        │
└───────────────┬────────────────────────────────────────────┘
                │  plain Go calls
┌───────────────▼────────────────────────────────────────────┐
│ internal/  (pure Go, no Wails, no frontend)                 │
│   notes/     document model + text statistics                │
│   settings/  preferences persisted as JSON                  │
│   apiserver/ HTTP control plane: key + IP allowlist         │
└────────────────────────────────────────────────────────────┘
```

### `internal/` — the reusable core

Pure Go with no dependency on Wails or the DOM, so it is trivially unit-testable
and portable across projects.

- **`notes/`** — the document model and text statistics, pure Go with no UI
  dependency. It computes the line / word / character counts a notepad shows and
  backs the stateless `POST /v1/stats` endpoint. `Stats(text) (...)` is the
  whole surface.
- **`settings/`** — a plain struct persisted as JSON in the OS config dir
  (`os.UserConfigDir`). Framework-agnostic; it knows nothing about the app that
  uses it.
- **`apiserver/`** — a small `net/http` server whose app-specific behaviour is
  *injected* as interfaces (`TextFunc`, `InfoFunc`, `UIController`). The same
  server powers any app in the template; you provide the handlers.

### The adapter — the only Wails-aware layer

- **`app.go`** constructs the core, holds the Wails `ctx`, and exposes methods to
  the frontend (compute text stats, read/write settings, start/stop the server,
  manage the allowlist). It is deliberately thin: translation, not logic.
- **`appinfo.go`** builds the `/v1/ax` descriptor — the machine-readable map of
  the app (see [agent-api.md](agent-api.md)).
- **`uibridge.go`** implements `UIController`. When the REST server asks it to
  press a button, it emits a Wails event to the webview, the frontend performs
  the DOM action and calls back with the resulting on-screen state.

### `frontend/` — the dumb UI

Vue 3 with `<script setup lang="ts">`. A single reactive object in `store.ts`
acts as the router for this single-window app. Views forward user intent to Go
and render whatever comes back. Every interactive element carries a stable
`data-testid` so both humans and agents address controls the same way.

## The UI bridge

This is what makes the app *agent-operable* rather than just *scriptable*. An
agent does not simulate the OS or move a mouse — it asks the app to operate
itself:

```
REST client            Go (uibridge.go)                Vue (uibridge.ts)
    │  POST /v1/ui/press     │                                  │
    │───────────────────────▶│  emit "ui:command" {id, press}   │
    │                        │─────────────────────────────────▶│  find [data-testid], .click()
    │                        │                                  │  wait until settled (see below)
    │                        │  UIAck(id, stateJSON)  ◀──────────│  read the DOM back
    │  resulting state  ◀─────│                                  │
```

Two design points worth keeping when reusing this:

- **Settling, not sleeping.** The bridge does not guess a fixed delay before
  reading the screen. The frontend keeps a `busy` counter that async handlers
  increment/decrement around their Go round-trips; the bridge waits (bounded)
  until it drains, then reads. Synchronous updates return after one `nextTick`.
- **Serialization.** Commands run one at a time — a queue on the frontend and a
  mutex on the Go dispatch — so two DOM mutations never overlap and corrupt each
  other's read-back.

## Adapting the template to a new app

1. Replace `internal/notes` with your engine (pure Go, unit-tested).
2. Replace the Vue views with your UI; keep `data-testid` on every control.
3. Rewrite `appInfo()` in `appinfo.go` to describe the new controls and actions.
4. Keep the shared libraries and the security model as they are — that is the
   framework: the REST control plane comes from
   [go-apiserver](https://github.com/viniciusbuscacio/go-apiserver), the
   self-update from
   [go-updates](https://github.com/viniciusbuscacio/go-updates);
   `internal/settings` and `uibridge.go` stay app-local. Register your domain
   endpoints with `HandleExtra` (see `handleStats` in `app.go`).

## Platform notes

- **Frameless window.** `main.go` runs a custom title bar; the taskbar icon on
  Windows is fixed at runtime (`icon_windows.go`, `WM_SETICON`) because a
  frameless Wails window does not pick up the embedded icon on its own.
- **Transparency.** Window translucency is driven by a CSS alpha token so the
  slider in Settings changes opacity without a rebuild.
