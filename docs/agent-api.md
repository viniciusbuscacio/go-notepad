# Agent API — the control plane

go-Notepad exposes a small HTTP API so an AI agent (or any script) can discover the
app and operate it end-to-end. It is **off by default**; start it from
**Settings → REST API Server**.

Every request needs the header `X-API-Key: <key>` (shown, copyable, in that same
panel) and must come from an IP in the allowlist (default `127.0.0.1/32`). See
[security.md](security.md).

Base URL is shown in the panel; by default `http://127.0.0.1:8837`.

## Start here: `GET /v1/ax`

One request tells the agent everything. The response is a JSON document:

| Field | Meaning |
| --- | --- |
| `schemaVersion` | integer contract version of this document — bumped on any breaking change |
| `version` | app/framework version |
| `app`, `description` | what this is |
| `howToUse` | prose instructions (how to type, tabs, disk I/O, how errors and risk work) |
| `capabilities` | e.g. `["notes.stats","ui.state","ui.press","ui.dblclick","ui.key","ui.input"]` |
| `api` | the endpoint list with example bodies |
| `errors` | every error `code`, its HTTP `status`, and its meaning |
| `axTree` | the accessibility tree: every view and control |

### The accessibility tree

Each node describes one view or control:

```json
{
  "role": "button",
  "name": "New tab",
  "testid": "new-tab",
  "action": "open a new empty document in another tab",
  "keyboard": "Ctrl+N",
  "risk": "safe"
}
```

- **`testid`** is how you address the control in `/v1/ui/*`.
- **`keyboard`** is the physical key (or shortcut) that does the same thing —
  pass it straight to `/v1/ui/key`, modifiers included (e.g. `"Ctrl+N"`).
- **`risk`** tells you how careful to be before pressing (see below).
- Views carry `openedBy` / `id` so you know how to reach controls that are not on
  the current screen.

### Risk levels

| Risk | Meaning | Examples |
| --- | --- | --- |
| `safe` | no lasting effect | typing, theme, font, copy public text |
| `navigation` | only moves between views | `open-settings`, `nav-api`, `back` |
| `external` | reaches outside the app | `open-github` (opens a browser) |
| `sensitive` | changes exposure, writes to disk, or reveals a secret | `save-file`, `toggle-server`, `add-ip`, `rotate-key`, `copy-key` |
| `destructive` | irreversible / closes the app | `window-close` |

An agent should treat anything above `navigation` as needing intent or
confirmation.

## Count text directly: `POST /v1/stats`

```
POST /v1/stats   {"text":"hello world"}   ->   200 {"lines":1,"words":2,"chars":11,"charsNoSpaces":10}
```

Stateless: it counts whatever text you send without touching the open document.
An empty string is valid and returns all-zero counts.

## Drive the real UI: `/v1/ui/*`

These operate the actual frontend and return the resulting on-screen **state**
(the same shape `GET /v1/ui/state` returns): current `view`, `theme`, `opacity`,
the list of `controls` currently on screen, and view-specific fields like
`text` (the active document), `tabs`, `serverStatus`, `allowlist`, `inputs`.

| Method | Path | Body | Does |
| --- | --- | --- | --- |
| `GET` | `/v1/ui/state` | — | read what is on screen |
| `POST` | `/v1/ui/press` | `{"testid":"new-tab"}` | click a control |
| `POST` | `/v1/ui/dblclick` | `{"testid":"titlebar"}` | double-click a control |
| `POST` | `/v1/ui/key` | `{"key":"Ctrl+N"}` | send a keystroke/shortcut to the app |
| `POST` | `/v1/ui/input` | `{"testid":"editor","value":"hello"}` | type into a field (empty `value` clears it) |

The bridge waits for the UI to finish updating — including any Go round-trip
triggered by the action — before it reads the state back, so the state you
receive reflects the completed action, not a half-rendered frame. Commands are
serialized, so it is safe to fire them back-to-back.

### Example: type into the document, then read it back

```bash
curl -X POST $BASE/v1/ui/input -H "X-API-Key: $KEY" \
  -d '{"testid":"editor","value":"Hello from an agent"}'
curl -X POST $BASE/v1/ui/key   -H "X-API-Key: $KEY" -d '{"key":"Ctrl+N"}'   # new tab
curl -H "X-API-Key: $KEY" $BASE/v1/ui/state
# {"view":"editor","text":"Hello from an agent","tabs":[...], ...}
```

## Errors

Every error has the same shape; branch on `code`, not on the message text:

```json
{ "error": { "code": "unknown_testid", "message": "unknown testid: key-x", "status": 404 } }
```

| `code` | HTTP | When |
| --- | --- | --- |
| `invalid_json` | 400 | body is not valid JSON |
| `missing_field` | 400 | a required field (`text` / `testid` / `key`) was empty or absent |
| `unauthorized` | 401 | invalid or missing `X-API-Key` |
| `forbidden` | 403 | client IP not in the allowlist |
| `unknown_testid` | 404 | no control on screen has that `testid` |
| `method_not_allowed` | 405 | wrong HTTP method (these endpoints are POST) |
| `disabled_control` | 409 | the control exists but is currently disabled |
| `operation_error` | 422 | the `/v1/stats` operation failed |
| `ui_timeout` | 503 | the UI did not respond in time |

The authoritative list is always the `errors` array in `GET /v1/ax` for the
running version.

## Verifying the contract

`tools/smoke` exercises all of the above — health, auth, the `/v1/ax`
fields, direct text stats, driving the UI, every structured error, and a check
that **every `testid` advertised in `/v1/ax` is reachable on screen**. Run it
with the app open and the server started:

```bash
go run ./tools/smoke
```
