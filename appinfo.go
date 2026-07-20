package main

// appInfo builds the app descriptor / accessibility tree served at GET /v1/ax.
// It tells an automated client what the app is, how to use it, and — via the
// axTree — every view and control with its role, testid, action, keyboard
// shortcut and risk level, so an agent knows exactly where to "click" and which
// actions to treat with care.

// axSchemaVersion is the contract version of the /v1/ax document. Bump it on any
// breaking change to the shape below so clients can detect drift.
const axSchemaVersion = 1

// appVersion is the app/framework version reported in /v1/ax. It is a var, not
// a const, so release builds can stamp the real tag over the dev default via
// -ldflags "-X main.appVersion=X.Y.Z" (see .github/workflows/release.yml).
var appVersion = "0.1.0"

// Risk levels classify how careful a client should be before invoking a control:
//
//	safe        no lasting effect (typing, tabs, theme, copy public text)
//	navigation  only moves between views
//	external    reaches outside the app (opens a browser)
//	sensitive   changes security/exposure or reveals a secret (server, allowlist, key)
//	destructive irreversible or closes the app
const (
	riskSafe        = "safe"
	riskNavigation  = "navigation"
	riskExternal    = "external"
	riskSensitive   = "sensitive"
	riskDestructive = "destructive"
)

type axNode struct {
	Role        string   `json:"role"`
	Name        string   `json:"name"`
	ID          string   `json:"id,omitempty"`
	Testid      string   `json:"testid,omitempty"`
	Description string   `json:"description,omitempty"`
	Action      string   `json:"action,omitempty"`
	Keyboard    string   `json:"keyboard,omitempty"`
	Risk        string   `json:"risk,omitempty"`
	OpenedBy    string   `json:"openedBy,omitempty"`
	Children    []axNode `json:"children,omitempty"`
}

type apiEndpoint struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Body    any    `json:"body,omitempty"`
	Returns any    `json:"returns,omitempty"`
	Auth    string `json:"auth"`
}

// errorInfo documents one stable error code an automated client may receive.
type errorInfo struct {
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Meaning string `json:"meaning"`
}

type appInfoDTO struct {
	SchemaVersion int           `json:"schemaVersion"`
	Version       string        `json:"version"`
	App           string        `json:"app"`
	Description   string        `json:"description"`
	HowToUse      string        `json:"howToUse"`
	Capabilities  []string      `json:"capabilities"`
	API           []apiEndpoint `json:"api"`
	Errors        []errorInfo   `json:"errors"`
	AXTree        axNode        `json:"axTree"`
}

func (a *App) appInfo() any {
	editorChildren := []axNode{
		{Role: "button", Name: "New tab", Testid: "new-tab", Action: "open a new empty document in another tab", Keyboard: "Ctrl+N", Risk: riskSafe},
		{Role: "button", Name: "Open", Testid: "open-file", Action: "open a text file from disk into a new tab (shows a native file dialog)", Keyboard: "Ctrl+O", Risk: riskSafe},
		{Role: "button", Name: "Save", Testid: "save-file", Action: "save the active document to disk (a native dialog asks for a path if it is untitled; Ctrl+Shift+S always asks, i.e. Save As)", Keyboard: "Ctrl+S", Risk: riskSensitive},
		{
			Role:        "tablist",
			Name:        "Open documents",
			Testid:      "tablist",
			Description: "One tab per open document, in order. Switch with testid 'tab-N' and close with 'close-N', where N is the 0-based position (tab-0 is the first). The current tabs, their names and which is active are in ui state under 'tabs'.",
		},
		{Role: "textbox", Name: "Editor", Testid: "editor", Action: "the active document's text; type here with /v1/ui/input (testid 'editor') and read it back from ui state field 'text'", Risk: riskSafe},
		{Role: "status", Name: "Status bar", Testid: "statusbar", Description: "left: the active file's path; right: cursor line/column, word and character counts, font size"},
		{Role: "status", Name: "File path", Testid: "file-path", Description: "full path of the active document, or its name while unsaved, at the left of the status bar"},
		{Role: "status", Name: "Font size", Testid: "font-size-status", Description: "current editor font size in px, at the right of the status bar"},
		{Role: "status", Name: "File error", Testid: "file-error", Description: "the last file open/save failure (operation + file); present only after a failure, in the status bar"},
		{Role: "dialog", Name: "Save changes?", Testid: "close-confirm", Description: "present only while closing a tab with unsaved changes: buttons 'confirm-save' (save, then close), 'confirm-discard' (close without saving) and 'confirm-cancel' (keep the tab open)"},
	}

	return appInfoDTO{
		SchemaVersion: axSchemaVersion,
		Version:       appVersion,
		App:           "go-Notepad",
		Description:   "A Windows 11-style notepad built with Go + Wails and TypeScript: tabbed plain-text editing with open/save. Also a template for cross-platform desktop apps.",
		HowToUse: "Edit plain text in tabs. Each tab is a document; the active tab's text is the 'editor' textbox. " +
			"To type, POST to /v1/ui/input with testid 'editor' and the full new text as 'value'; read the current text back from ui state field 'text'. " +
			"Send individual keystrokes with /v1/ui/key; the 'key' may include modifier prefixes to trigger the app's shortcuts (e.g. 'Ctrl+N' new tab, 'Ctrl+O' open, 'Ctrl+S' save, 'Ctrl+Shift+S' save as, 'Ctrl+W' close tab). Press 'new-tab' to add a tab, 'open-file' / 'save-file' for disk I/O (these show native dialogs), " +
			"'tab-N' to switch to the N-th tab and 'close-N' to close it (0-based); closing a tab with unsaved changes opens a confirm dialog with 'confirm-save', 'confirm-discard' and 'confirm-cancel'. " +
			"For a stateless word/line/character count of any text (without touching the open document), POST it to /v1/stats as {\"text\": \"...\"}. " +
			"Every control carries a 'risk' level (safe, navigation, external, sensitive, destructive) — check it before pressing; e.g. window-close is destructive and save-file writes to disk. " +
			"Errors are structured as {\"error\":{\"code\",\"message\",\"status\"}}; branch on 'code' (see the errors list). " +
			"Pressing an unknown testid returns code unknown_testid (404); a disabled control returns disabled_control (409). " +
			"The title-bar gear (open-settings) opens Settings; each panel has a back button. " +
			"Settings also hosts the in-app updater: press 'update-check' to query GitHub Releases (risk external) and read the outcome at GET /v1/update; " +
			"'update-install' (risk destructive) downloads, verifies and applies the new version, then RESTARTS the app — the API goes away mid-call.",
		Capabilities: []string{"notes.stats", "ui.state", "ui.press", "ui.dblclick", "ui.key", "ui.input", "updates"},
		Errors: []errorInfo{
			{Code: "invalid_json", Status: 400, Meaning: "request body is not valid JSON"},
			{Code: "missing_field", Status: 400, Meaning: "a required field (testid / key) was empty or absent"},
			{Code: "unauthorized", Status: 401, Meaning: "invalid or missing X-API-Key header"},
			{Code: "forbidden", Status: 403, Meaning: "the client IP is not in the allowlist"},
			{Code: "unknown_testid", Status: 404, Meaning: "no control on screen has that testid"},
			{Code: "method_not_allowed", Status: 405, Meaning: "wrong HTTP method (these endpoints are POST)"},
			{Code: "disabled_control", Status: 409, Meaning: "the control exists but is currently disabled"},
			{Code: "operation_error", Status: 422, Meaning: "the /v1/stats operation failed"},
			{Code: "ui_timeout", Status: 503, Meaning: "the UI did not respond in time"},
		},
		API: []apiEndpoint{
			{Method: "POST", Path: "/v1/stats", Body: map[string]string{"text": "hello world"}, Returns: map[string]int{"lines": 1, "words": 2, "chars": 11, "charsNoSpaces": 10}, Auth: "X-API-Key header"},
			{Method: "GET", Path: "/v1/health", Returns: map[string]string{"status": "ok"}, Auth: "X-API-Key header"},
			{Method: "GET", Path: "/v1/ax", Returns: "this document (app info + accessibility tree)", Auth: "X-API-Key header"},
			{Method: "GET", Path: "/v1/update", Returns: "last update-check snapshot: {checking, installing, available, version, notes, current, checkedAt, error, notify}", Auth: "X-API-Key header"},
			{Method: "GET", Path: "/v1/ui/state", Returns: "current on-screen state (view, tabs, editor text, ...)", Auth: "X-API-Key header"},
			{Method: "POST", Path: "/v1/ui/press", Body: map[string]string{"testid": "new-tab"}, Returns: "resulting on-screen state", Auth: "X-API-Key header"},
			{Method: "POST", Path: "/v1/ui/dblclick", Body: map[string]string{"testid": "titlebar"}, Returns: "resulting on-screen state (double-clicking the title bar maximizes/restores the window)", Auth: "X-API-Key header"},
			{Method: "POST", Path: "/v1/ui/key", Body: map[string]string{"key": "Ctrl+N"}, Returns: "resulting on-screen state", Auth: "X-API-Key header"},
			{Method: "POST", Path: "/v1/ui/input", Body: map[string]string{"testid": "editor", "value": "new document text"}, Returns: "resulting on-screen state", Auth: "X-API-Key header"},
		},
		AXTree: axNode{
			Role: "application",
			Name: "go-Notepad",
			Children: []axNode{
				{
					Role:        "toolbar",
					Name:        "Title bar",
					Testid:      "titlebar",
					Description: "Always visible, on every view. Double-clicking it (POST /v1/ui/dblclick testid 'titlebar') maximizes/restores the window.",
					Children: []axNode{
						{Role: "button", Name: "API server indicator", Testid: "api-indicator", Action: "open the REST API server settings (green dot, visible only while this server is running)", Risk: riskNavigation},
						{Role: "button", Name: "Settings", Testid: "open-settings", Action: "open the Settings view", Risk: riskNavigation},
						{Role: "button", Name: "Minimize", Testid: "window-minimize", Action: "minimize the window", Risk: riskSafe},
						{Role: "button", Name: "Maximize", Testid: "window-maximize", Action: "maximize/restore the window", Risk: riskSafe},
						{Role: "button", Name: "Close", Testid: "window-close", Action: "close the app", Risk: riskDestructive},
					},
				},
				{
					Role:        "view",
					Name:        "Editor",
					ID:          "editor",
					Description: "Main screen: a tabbed plain-text editor. The tab strip sits on top or on the left (a Settings choice). Type into the 'editor' textbox; the status bar shows counts.",
					Children:    editorChildren,
				},
				{
					Role:        "view",
					Name:        "Settings",
					ID:          "options",
					OpenedBy:    "open-settings",
					Description: "Opened by the gear button in the title bar.",
					Children: []axNode{
						{Role: "button", Name: "Back", Testid: "back", Action: "return to the editor", Risk: riskNavigation},
						{Role: "switch", Name: "Dark mode", Testid: "theme-switch", Action: "toggle between dark and light theme", Risk: riskSafe},
						{Role: "slider", Name: "Transparency", Testid: "opacity-slider", Action: "set window opacity from 20% to 100%", Risk: riskSafe},
						{Role: "button", Name: "Tabs on top", Testid: "tab-position-top", Action: "put the tab strip on top (horizontal)", Risk: riskSafe},
						{Role: "button", Name: "Tabs on left", Testid: "tab-position-left", Action: "put the tab strip on the left (vertical, Edge-style)", Risk: riskSafe},
						{Role: "switch", Name: "Word wrap", Testid: "wordwrap-switch", Action: "wrap long lines in the editor", Risk: riskSafe},
						{Role: "combobox", Name: "Font", Testid: "font-family", Action: "choose the editor typeface (use /v1/ui/input with a font key: 'mono' for the system default, or a bundled Google Font id such as 'inter', 'roboto', 'jetbrains-mono', 'merriweather')", Risk: riskSafe},
						{Role: "button", Name: "Font smaller", Testid: "font-size-dec", Action: "decrease the editor font size (also Ctrl+- or Ctrl+wheel in the editor)", Risk: riskSafe},
						{Role: "status", Name: "Font size value", Testid: "font-size", Description: "the current editor font size between the stepper buttons"},
						{Role: "button", Name: "Font bigger", Testid: "font-size-inc", Action: "increase the editor font size (also Ctrl++ or Ctrl+wheel in the editor)", Risk: riskSafe},
						{Role: "text", Name: "Font preview", Testid: "font-preview", Description: "sample line rendered in the selected font and size"},
						{Role: "button", Name: "REST API Server", Testid: "nav-api", Action: "open the REST API server settings", Risk: riskNavigation},
						{Role: "switch", Name: "Automatic update checks", Testid: "update-autocheck", Action: "toggle checking GitHub for a newer release once a day on launch (off by default; checking calls the network)", Risk: riskSafe},
						{Role: "button", Name: "Check for updates", Testid: "update-check", Action: "ask GitHub Releases for a newer version right now; the result (including 'notify') is also served at GET /v1/update", Risk: riskExternal},
						{Role: "status", Name: "Update status", Testid: "update-status", Description: "outcome of the last update check: up to date, update available, or the error"},
						{Role: "text", Name: "Release notes", Testid: "update-notes", Description: "the newer version's release notes; present only while an update is available"},
						{Role: "button", Name: "Install and restart", Testid: "update-install", Action: "download the new version, verify its checksum, replace the app and restart it; present only while an update is available", Risk: riskDestructive},
						{Role: "button", Name: "Skip this version", Testid: "update-skip", Action: "silence this particular version (a newer one will notify again); present only while an update is available", Risk: riskSafe},
						{Role: "button", Name: "Remind me later", Testid: "update-later", Action: "snooze the update notice for 7 days; present only while an update is available", Risk: riskSafe},
						{Role: "link", Name: "GitHub", Testid: "open-github", Action: "open the project on GitHub in the default browser", Risk: riskExternal},
						{Role: "text", Name: "Version", Testid: "app-version", Description: "the app version (About section)"},
					},
				},
				{
					Role:        "view",
					Name:        "REST API Server",
					ID:          "api",
					Description: "Opened from Settings → REST API Server.",
					Children: []axNode{
						{Role: "button", Name: "Back", Testid: "back", Action: "return to Settings", Risk: riskNavigation},
						{Role: "button", Name: "Start/Stop", Testid: "toggle-server", Action: "start or stop the REST server", Risk: riskSensitive},
						{Role: "status", Name: "Server status", Testid: "status", Description: "shows Running or Stopped"},
						{Role: "status", Name: "Server error", Testid: "server-error", Description: "the last start/stop/apply failure; present only after a failure"},
						{Role: "button", Name: "Shuffle port", Testid: "shuffle-port", Action: "pick a random free port (8800-8899) and restart the server if running", Risk: riskSensitive},
						{Role: "switch", Name: "Start automatically", Testid: "autostart", Action: "toggle starting the server on app launch", Risk: riskSensitive},
						{Role: "switch", Name: "Use HTTPS", Testid: "use-https", Action: "toggle serving HTTPS with a self-signed pinned cert instead of plain HTTP (restarts the server if running)", Risk: riskSensitive},
						{Role: "table", Name: "Allowed IPs", Testid: "allowlist", Description: "CIDR allowlist controlling who may call the API. Each row has a remove button with testid 'remove-<cidr>' (e.g. 'remove-127.0.0.1/32'); removing an entry is sensitive (it changes exposure) and the last entry cannot be removed. The current entries are in ui state under 'allowlist'."},
						{Role: "textbox", Name: "New IP", Testid: "new-ip", Action: "type a CIDR (e.g. 192.168.0.0/24) to allow", Risk: riskSafe},
						{Role: "button", Name: "Add IP", Testid: "add-ip", Action: "add the typed CIDR to the allowlist", Risk: riskSensitive},
						{Role: "status", Name: "Allowlist error", Testid: "ip-error", Description: "the last allowlist add/remove failure; present only after a failure"},
						{Role: "text", Name: "Agent instructions", Testid: "agent-instructions", Description: "copy-paste snippet: base URL, key, and starting endpoints"},
						{Role: "button", Name: "Copy instructions", Testid: "copy-instructions", Action: "copy the agent instructions", Risk: riskSafe},
						{Role: "text", Name: "Access key", Testid: "api-key", Description: "the API key (masked)"},
						{Role: "button", Name: "Copy key", Testid: "copy-key", Action: "copy the API key", Risk: riskSensitive},
						{Role: "button", Name: "Rotate key", Testid: "rotate-key", Action: "generate a new API key", Risk: riskSensitive},
						{Role: "group", Name: "HTTPS details", Testid: "tls-section", Description: "certificate pin and curl test command; present only while HTTPS is on"},
						{Role: "text", Name: "Certificate pin", Testid: "fingerprint", Description: "the TLS public-key pin (shortened); present only while HTTPS is on"},
						{Role: "button", Name: "Copy pin", Testid: "copy-fingerprint", Action: "copy the full TLS pin (sha256//...)", Description: "present only while HTTPS is on", Risk: riskSafe},
						{Role: "text", Name: "Test command", Testid: "curl-example", Description: "a ready-to-run curl with the pin baked in; present only while HTTPS is on"},
						{Role: "button", Name: "Copy test command", Testid: "copy-curl", Action: "copy the curl test command", Description: "present only while HTTPS is on", Risk: riskSafe},
					},
				},
			},
		},
	}
}
