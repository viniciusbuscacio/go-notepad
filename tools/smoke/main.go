// Command smoke is the end-to-end smoke test for the go-Notepad agent control
// plane. It discovers the API key, port and scheme from the app's settings
// file (or takes them via -base-url / -api-key), then exercises every layer an
// agent depends on:
//
//   - /v1/health         reachability + auth
//   - /v1/ax             the app descriptor / accessibility tree contract
//   - /v1/stats          direct text statistics (word/line/char counts)
//   - /v1/ui/*           driving the REAL UI (press, key, input, state)
//   - structured errors  invalid_json, missing_field, unknown_testid,
//     disabled_control
//   - AX <-> DOM coverage: every unconditional testid advertised in /v1/ax is
//     reachable on screen
//
// The typing test runs in a tab of its own (opened, typed into, then discarded
// via the confirm dialog), so the user's open document is never touched.
//
// Run it while the app is open with the REST server started:
//
//	go run ./tools/smoke
//	go run ./tools/smoke -base-url https://127.0.0.1:8837 -api-key <key>
//	go run ./tools/smoke -pin "sha256//..."   # verify the self-signed cert
//
// The app's self-signed certificate is accepted for the localhost smoke run;
// pass -pin (the value the API panel shows) to actually verify it.
//
// Exit code is 0 when every check passes, non-zero otherwise (1 = a check
// failed or the server is unreachable, 2 = no API key found).
package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const appDir = "go-notepad"

type settings struct {
	APIPort  int    `json:"apiPort"`
	APIKey   string `json:"apiKey"`
	APIHTTPS bool   `json:"apiHttps"`
}

// loadSettings reads the app's settings.json from the per-user config dir —
// the same location the app writes (os.UserConfigDir matches the app's own
// platform logic). Missing or unreadable settings just mean zero values.
func loadSettings() settings {
	var s settings
	dir, err := os.UserConfigDir()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(filepath.Join(dir, appDir, "settings.json"))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

// client is a minimal JSON API client. HTTP-level errors (4xx/5xx) are
// returned as (status, parsed body), not as Go errors — the checks assert on
// them; only transport failures return err.
type client struct {
	base string
	key  string
	http *http.Client
}

func newClient(base, key, pin string) *client {
	tlsCfg := &tls.Config{
		// The app serves a self-signed cert; trust-on-first-use for a local
		// smoke run. With -pin the public key is verified for real below.
		InsecureSkipVerify: true,
	}
	if pin != "" {
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
			got := "sha256//" + base64.StdEncoding.EncodeToString(sum[:])
			if got != pin {
				return fmt.Errorf("TLS pin mismatch: server has %s", got)
			}
			return nil
		}
	}
	return &client{
		base: strings.TrimRight(base, "/"),
		key:  key,
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}
}

// call performs a request and returns (status, decoded JSON). body may be any
// JSON-marshalable value, or a raw string sent verbatim (to test invalid_json).
// key overrides the client's API key when non-empty (to test auth rejection).
func (c *client) call(method, path string, body any, key string) (int, any, error) {
	var reader io.Reader
	if body != nil {
		if raw, ok := body.(rawBody); ok {
			reader = strings.NewReader(string(raw))
		} else {
			data, err := json.Marshal(body)
			if err != nil {
				return 0, nil, err
			}
			reader = bytes.NewReader(data)
		}
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if key == "" {
		key = c.key
	}
	req.Header.Set("X-API-Key", key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	var parsed any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			parsed = string(raw)
		}
	}
	return resp.StatusCode, parsed, nil
}

// rawBody marks a request body to be sent as-is instead of JSON-marshaled.
type rawBody string

func (c *client) get(path string) (int, any, error) { return c.call("GET", path, nil, "") }

func (c *client) press(testid string) (int, any, error) {
	return c.call("POST", "/v1/ui/press", map[string]string{"testid": testid}, "")
}

func (c *client) key_(k string) (int, any, error) {
	return c.call("POST", "/v1/ui/key", map[string]string{"key": k}, "")
}

func (c *client) put(testid, value string) (int, any, error) {
	return c.call("POST", "/v1/ui/input", map[string]string{"testid": testid, "value": value}, "")
}

// checks tallies pass/fail and prints one line per assertion.
type checks struct{ passed, failed int }

func (ch *checks) ok(name string, cond bool, detail string) bool {
	mark := "PASS"
	if !cond {
		mark = "FAIL"
		ch.failed++
		if detail != "" {
			name += "  -> " + detail
		}
	} else {
		ch.passed++
	}
	fmt.Printf("[%s] %s\n", mark, name)
	return cond
}

// ---- JSON helpers (the API hands back free-form documents) ----

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func errCode(v any) string {
	e := asMap(asMap(v)["error"])
	code, _ := e["code"].(string)
	return code
}

func editorText(state any) string {
	t, _ := asMap(state)["text"].(string)
	return t
}

func tabCount(state any) int {
	tabs, _ := asMap(state)["tabs"].([]any)
	return len(tabs)
}

func controls(state any) map[string]bool {
	out := map[string]bool{}
	list, _ := asMap(state)["controls"].([]any)
	for _, v := range list {
		if s, ok := v.(string); ok {
			out[s] = true
		}
	}
	return out
}

// collectTestids gathers the testids in the ax tree, skipping controls the
// tree itself documents as conditional ("present only ...", e.g. the TLS pin
// fields or error lines) — those are not expected on screen at any given
// moment, so they are left out of the AX <-> DOM coverage check.
func collectTestids(node map[string]any, out map[string]bool) {
	desc, _ := node["description"].(string)
	conditional := strings.Contains(strings.ToLower(desc), "present only")
	if tid, _ := node["testid"].(string); tid != "" && !conditional {
		out[tid] = true
	}
	children, _ := node["children"].([]any)
	for _, c := range children {
		if m := asMap(c); m != nil {
			collectTestids(m, out)
		}
	}
}

func main() {
	baseURL := flag.String("base-url", "", "e.g. https://127.0.0.1:8837 (default: derived from settings.json)")
	apiKey := flag.String("api-key", "", "X-API-Key value (default: read from settings.json)")
	pin := flag.String("pin", "", `TLS public-key pin to verify, e.g. "sha256//..." (default: accept the self-signed cert)`)
	flag.Parse()

	s := loadSettings()
	key := *apiKey
	if key == "" {
		key = s.APIKey
	}
	if key == "" {
		fmt.Println("No API key found. Pass -api-key or open the app once so it writes settings.json.")
		os.Exit(2)
	}
	base := *baseURL
	if base == "" {
		scheme := "http"
		if s.APIHTTPS {
			scheme = "https"
		}
		port := s.APIPort
		if port == 0 {
			port = 8837
		}
		base = fmt.Sprintf("%s://127.0.0.1:%d", scheme, port)
	}

	fmt.Println("Target:", base)
	c := newClient(base, key, *pin)
	ch := &checks{}

	// --- health + auth ---
	st, body, err := c.get("/v1/health")
	if !ch.ok("health reachable", err == nil && st == 200, fmt.Sprintf("status=%d err=%v", st, err)) {
		fmt.Println("Server not reachable — is the app open with the REST server started?")
		os.Exit(1)
	}
	status, _ := asMap(body)["status"].(string)
	ch.ok("health body ok", status == "ok", fmt.Sprint(body))

	st, body, _ = c.call("GET", "/v1/health", nil, "wrong-key")
	ch.ok("auth rejects bad key (401 unauthorized)",
		st == 401 && errCode(body) == "unauthorized", fmt.Sprintf("status=%d body=%v", st, body))

	// --- ax contract ---
	st, ax, _ := c.get("/v1/ax")
	ch.ok("ax reachable", st == 200, fmt.Sprintf("status=%d", st))
	axDoc := asMap(ax)
	_, hasSchema := axDoc["schemaVersion"]
	ch.ok("ax has schemaVersion", hasSchema, "")
	version, _ := axDoc["version"].(string)
	ch.ok("ax has version", version != "", "")
	caps, _ := axDoc["capabilities"].([]any)
	hasStats := false
	for _, v := range caps {
		if v == "notes.stats" {
			hasStats = true
		}
	}
	ch.ok("ax advertises capabilities", hasStats, "")
	codes, _ := axDoc["errors"].([]any)
	hasUnknown := false
	for _, v := range codes {
		if code, _ := asMap(v)["code"].(string); code == "unknown_testid" {
			hasUnknown = true
		}
	}
	ch.ok("ax documents error codes", hasUnknown, "")

	axTestids := map[string]bool{}
	if tree := asMap(axDoc["axTree"]); tree != nil {
		collectTestids(tree, axTestids)
	}
	ch.ok("ax tree exposes testids", len(axTestids) > 10, fmt.Sprintf("count=%d", len(axTestids)))

	// --- direct text stats ---
	stats := func(text string) (int, any) {
		st, body, _ := c.call("POST", "/v1/stats", map[string]string{"text": text}, "")
		return st, body
	}
	num := func(body any, field string) int {
		f, _ := asMap(body)[field].(float64)
		return int(f)
	}

	st, body = stats("hello world")
	ch.ok("stats counts words/chars (hello world)",
		st == 200 && num(body, "words") == 2 && num(body, "chars") == 11, fmt.Sprint(body))

	st, body = stats("a\nb\nc")
	ch.ok("stats counts lines (3)", st == 200 && num(body, "lines") == 3, fmt.Sprint(body))

	st, body = stats("")
	ch.ok("stats empty text -> all zero",
		st == 200 && num(body, "words") == 0 && num(body, "chars") == 0, fmt.Sprint(body))

	st, body, _ = c.call("POST", "/v1/stats", rawBody("not json"), "")
	ch.ok("stats invalid json -> 400 invalid_json",
		st == 400 && errCode(body) == "invalid_json", fmt.Sprintf("status=%d body=%v", st, body))

	// --- drive the real UI ---
	// The typing happens in a fresh tab so the user's document is untouched;
	// the dirty tab is then discarded through the confirm dialog, which also
	// exercises that flow.
	// The app may have been left on any view (e.g. the API panel, to copy the
	// key); two "back" presses always land on the home view. The 404 they
	// return when already home is harmless.
	c.press("back")
	c.press("back")
	st, state, _ := c.get("/v1/ui/state")
	view, _ := asMap(state)["view"].(string)
	ch.ok("ui state reachable on editor view", st == 200 && view == "editor", fmt.Sprintf("status=%d", st))

	before := tabCount(state)
	_, state, _ = c.press("new-tab")
	ch.ok("pressing new-tab adds a tab", tabCount(state) == before+1,
		fmt.Sprintf("tabs %d -> %d", before, tabCount(state)))

	_, state, _ = c.put("editor", "Hello from an agent")
	ch.ok("typing into the editor is read back", editorText(state) == "Hello from an agent",
		fmt.Sprintf("text=%q", editorText(state)))

	c.key_("Ctrl+W") // tab is dirty now, so this opens the confirm dialog
	_, state, _ = c.press("confirm-discard")
	ch.ok("Ctrl+W + confirm-discard closes the dirty tab", tabCount(state) == before,
		fmt.Sprintf("tabs -> %d", tabCount(state)))

	_, state, _ = c.key_("Ctrl+N")
	ch.ok("Ctrl+N shortcut opens a new tab", tabCount(state) == before+1,
		fmt.Sprintf("tabs -> %d", tabCount(state)))

	_, state, _ = c.key_("Ctrl+W") // still empty, closes without a dialog
	ch.ok("Ctrl+W shortcut closes the clean tab", tabCount(state) == before,
		fmt.Sprintf("tabs -> %d", tabCount(state)))

	st, body, _ = c.press("key-does-not-exist")
	ch.ok("unknown testid -> 404 unknown_testid",
		st == 404 && errCode(body) == "unknown_testid", fmt.Sprintf("status=%d body=%v", st, body))

	st, body, _ = c.key_("")
	ch.ok("empty key -> 400 missing_field",
		st == 400 && errCode(body) == "missing_field", fmt.Sprintf("status=%d body=%v", st, body))

	// --- in-app updater ---
	// The snapshot endpoint must serve the documented shape, and pressing the
	// Check button must settle into a structured outcome — "up to date",
	// "available" or a clean error (e.g. offline) are all acceptable; a hang or
	// an unstructured response is not. Point GO_NOTEPAD_UPDATE_URL at a stub
	// when launching the app to run this without touching the real GitHub.
	st, body, _ = c.get("/v1/update")
	ch.ok("update snapshot reachable", st == 200, fmt.Sprintf("status=%d", st))
	upd := asMap(body)
	_, hasNotify := upd["notify"].(bool)
	_, hasAvailable := upd["available"].(bool)
	cur, _ := upd["current"].(string)
	ch.ok("update snapshot has the contract fields", hasNotify && hasAvailable && cur != "",
		fmt.Sprint(body))

	c.press("open-settings")
	st, _, _ = c.press("update-check")
	ch.ok("pressing update-check is accepted", st == 200, fmt.Sprintf("status=%d", st))
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, body, _ = c.get("/v1/update")
		upd = asMap(body)
		if b, _ := upd["checking"].(bool); !b || time.Now().After(deadline) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	checking, _ := upd["checking"].(bool)
	updErr, _ := upd["error"].(string)
	_, hasAvailable = upd["available"].(bool)
	ch.ok("update check settles with a structured outcome",
		!checking && (updErr != "" || hasAvailable), fmt.Sprint(upd))
	c.press("back")

	// --- disabled control ---
	c.press("open-settings")
	c.press("nav-api")
	st, body, _ = c.press("add-ip") // 'New IP' is empty, so Add is disabled
	ch.ok("disabled control -> 409 disabled_control",
		st == 409 && errCode(body) == "disabled_control", fmt.Sprintf("status=%d body=%v", st, body))

	// --- AX <-> DOM coverage ---
	seen := map[string]bool{}
	_, state, _ = c.get("/v1/ui/state") // api view
	for t := range controls(state) {
		seen[t] = true
	}
	_, state, _ = c.press("back") // options view
	for t := range controls(state) {
		seen[t] = true
	}
	_, state, _ = c.press("back") // editor view
	for t := range controls(state) {
		seen[t] = true
	}
	var missing []string
	for t := range axTestids {
		if !seen[t] && !strings.Contains(t, "<") {
			missing = append(missing, t)
		}
	}
	sort.Strings(missing)
	ch.ok("every unconditional ax testid is reachable on screen", len(missing) == 0,
		fmt.Sprintf("missing=%v", missing))

	fmt.Printf("\n%d passed, %d failed\n", ch.passed, ch.failed)
	if ch.failed > 0 {
		os.Exit(1)
	}
}
