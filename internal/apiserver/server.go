// Package apiserver is a small, self-contained HTTP server guarded by an API
// key and an IP allowlist (CIDRs). The app-specific logic is injected as a
// function, so the same server powers any app in the framework — swap the
// injected handler(s) per project.
package apiserver

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TextFunc is the app-specific "direct" behaviour the server exposes at its
// primary domain endpoint. It takes a block of text and returns any
// JSON-serializable result (here, go-Notepad returns document statistics).
// Swap this per app to reuse the server unchanged.
type TextFunc func(text string) (any, error)

// InfoFunc returns the app descriptor / accessibility tree served at /v1/ax.
type InfoFunc func() any

// UIController drives the live frontend so a client can test the actual UI:
// read what is on screen, click controls by testid, press keys, type into
// inputs. Each call returns the resulting UI state. Implemented by the host app
// over its UI framework (here, Wails events → Vue).
type UIController interface {
	State() (any, error)
	Press(testid string) (any, error)
	DblClick(testid string) (any, error)
	Key(key string) (any, error)
	Input(testid, value string) (any, error)
}

// maxBodyBytes caps request bodies (1 MiB) before JSON decoding: plenty for
// any document this app handles, small enough to shrug off abuse.
const maxBodyBytes = 1 << 20

type Config struct {
	Port      int
	Key       string
	Allowlist []string
	TLS       bool   // serve HTTPS with a self-signed cert (used for LAN exposure)
	CertDir   string // where the stable TLS key lives; required when TLS is true
}

type Server struct {
	mu      sync.Mutex
	op      TextFunc
	info    InfoFunc
	ui      UIController
	httpSrv *http.Server
	cfg     Config
	running bool
	addr    string // actual bound address, once listening
	pin     string // public-key pin (base64 SHA-256 SPKI); empty unless TLS
	certPEM []byte // current self-signed cert, for --cacert export; nil unless TLS
}

func New(op TextFunc, info InfoFunc, ui UIController) *Server {
	return &Server{op: op, info: info, ui: ui}
}

func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Addr is the actual bound address (host:port), valid while running.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Fingerprint is the public-key pin a client fixes (base64 SHA-256 SPKI). Empty
// when serving plain HTTP.
func (s *Server) Fingerprint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pin
}

// CertPEM is the current self-signed certificate, for `curl --cacert` export.
// Nil when serving plain HTTP.
func (s *Server) CertPEM() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.certPEM
}

// buildTLS loads the stable key, mints a self-signed cert for the current SANs,
// records the pin + cert for the UI, and returns the tls.Config. Called with
// s.mu already held (by Start).
func (s *Server) buildTLS(cfg Config) (*tls.Config, error) {
	if cfg.CertDir == "" {
		return nil, fmt.Errorf("TLS requested but no CertDir configured")
	}
	key, err := loadOrCreateKey(cfg.CertDir)
	if err != nil {
		return nil, err
	}
	cert, certPEM, err := mintCert(key, sanIPs(cfg.Allowlist), []string{"localhost"})
	if err != nil {
		return nil, err
	}
	pin, err := publicKeyPin(key)
	if err != nil {
		return nil, err
	}
	s.pin, s.certPEM = pin, certPEM
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// Start binds and serves. It validates the allowlist up front so a bad CIDR
// surfaces to the UI instead of silently failing.
func (s *Server) Start(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}
	nets, err := parseCIDRs(cfg.Allowlist)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/stats", s.handleStats)
	mux.HandleFunc("/v1/ax", s.handleInfo)
	mux.HandleFunc("/v1/ui/state", s.handleUIState)
	mux.HandleFunc("/v1/ui/press", s.handleUIPress)
	mux.HandleFunc("/v1/ui/dblclick", s.handleUIDblClick)
	mux.HandleFunc("/v1/ui/key", s.handleUIKey)
	mux.HandleFunc("/v1/ui/input", s.handleUIInput)

	handler := withAllowlist(nets, withKey(cfg.Key, mux))

	// Bind to loopback when only localhost is allowed — this avoids the Windows
	// Firewall prompt entirely. We only open 0.0.0.0 (which triggers the prompt)
	// when the user has allowlisted a non-loopback IP, i.e. really wants LAN access.
	addr := fmt.Sprintf("%s:%d", BindHost(cfg.Allowlist), cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not open port %d: %w", cfg.Port, err)
	}

	s.pin, s.certPEM = "", nil
	if cfg.TLS {
		tlsCfg, err := s.buildTLS(cfg)
		if err != nil {
			_ = ln.Close()
			return err
		}
		ln = tls.NewListener(ln, tlsCfg)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.httpSrv = srv
	s.cfg = cfg
	s.addr = ln.Addr().String()
	s.running = true
	go func() { _ = srv.Serve(ln) }()
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	srv := s.httpSrv
	s.httpSrv = nil
	s.running = false
	// pin/addr/certPEM are only meaningful while running (the next Start may
	// mint a different cert); clear them so a stopped server never reports a
	// stale fingerprint or address.
	s.addr = ""
	s.pin, s.certPEM = "", nil
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// ---- handlers ----

// requireGet enforces the method on read-only endpoints, honouring the
// documented method_not_allowed contract.
func requireGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return false
	}
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleInfo serves the app descriptor / accessibility tree so an automated
// client can learn what the app does and how to drive it.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	if s.info == nil {
		writeErr(w, http.StatusNotFound, "not_found", "no app info")
		return
	}
	writeJSON(w, http.StatusOK, s.info())
}

type statsRequest struct {
	Text string `json:"text"`
}

// handleStats is the primary domain endpoint: given a block of text, return its
// line/word/character counts (the numbers a notepad shows in its status bar).
// Unlike the UI bridge it is stateless — it does not touch the open document.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	// Cap the body so a client can't feed the decoder an unbounded document.
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req statsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	// An empty string is a valid document (0 words), so 'text' is not required.
	result, err := s.op(req.Text)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "operation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---- UI bridge handlers ----
// These drive the real frontend: the client presses actual buttons and reads
// the actual screen, so it exercises the UI end-to-end (not just the engine).

func (s *Server) handleUIState(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	if s.ui == nil {
		writeErr(w, http.StatusNotImplemented, "not_implemented", "ui bridge not available")
		return
	}
	st, err := s.ui.State()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "ui_timeout", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleUIPress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Testid string `json:"testid"`
	}
	if !s.decodeUI(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Testid) == "" {
		writeErr(w, http.StatusBadRequest, "missing_field", "field 'testid' is required")
		return
	}
	state, err := s.ui.Press(req.Testid)
	s.writeUIResult(w, state, err)
}

func (s *Server) handleUIDblClick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Testid string `json:"testid"`
	}
	if !s.decodeUI(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Testid) == "" {
		writeErr(w, http.StatusBadRequest, "missing_field", "field 'testid' is required")
		return
	}
	state, err := s.ui.DblClick(req.Testid)
	s.writeUIResult(w, state, err)
}

func (s *Server) handleUIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if !s.decodeUI(w, r, &req) {
		return
	}
	if req.Key == "" {
		writeErr(w, http.StatusBadRequest, "missing_field", "field 'key' is required")
		return
	}
	state, err := s.ui.Key(req.Key)
	s.writeUIResult(w, state, err)
}

func (s *Server) handleUIInput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Testid string `json:"testid"`
		Value  string `json:"value"`
	}
	if !s.decodeUI(w, r, &req) {
		return
	}
	// 'value' is optional — an empty string is a valid input (clears the field).
	if strings.TrimSpace(req.Testid) == "" {
		writeErr(w, http.StatusBadRequest, "missing_field", "field 'testid' is required")
		return
	}
	state, err := s.ui.Input(req.Testid, req.Value)
	s.writeUIResult(w, state, err)
}

func (s *Server) decodeUI(w http.ResponseWriter, r *http.Request, req any) bool {
	if s.ui == nil {
		writeErr(w, http.StatusNotImplemented, "not_implemented", "ui bridge not available")
		return false
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return false
	}
	// Cap the body so a client can't feed the decoder an unbounded document.
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return false
	}
	return true
}

// writeUIResult turns the bridge outcome into an HTTP response. A transport
// failure (the frontend never answered) is a ui_timeout. An application-level
// problem is reported by the frontend as a structured {"error":{code,message}}
// field inside the returned state; we map its code to the right HTTP status so
// an automated client can react (unknown control → 404, disabled → 409, …).
func (s *Server) writeUIResult(w http.ResponseWriter, state any, err error) {
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "ui_timeout", err.Error())
		return
	}
	if raw, ok := state.(json.RawMessage); ok {
		var probe struct {
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Error != nil {
			code := probe.Error.Code
			if code == "" {
				code = "ui_error"
			}
			writeErr(w, statusForCode(code), code, probe.Error.Message)
			return
		}
	}
	writeJSON(w, http.StatusOK, state)
}

// statusForCode maps an application-level error code reported by the frontend
// to an HTTP status.
func statusForCode(code string) int {
	switch code {
	case "unknown_testid":
		return http.StatusNotFound
	case "disabled_control":
		return http.StatusConflict
	case "missing_field":
		return http.StatusBadRequest
	default:
		return http.StatusUnprocessableEntity
	}
}

// ---- middleware ----

func withKey(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Constant-time compare so a wrong key can't be recovered byte-by-byte
		// from response timing. Length mismatch fails fast, which is fine — the
		// key length isn't secret.
		provided := r.Header.Get("X-API-Key")
		if key == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid or missing X-API-Key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withAllowlist(nets []*net.IPNet, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ipAllowed(clientIP(r), nets) {
			writeErr(w, http.StatusForbidden, "forbidden", "client IP is not in the allowlist")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func parseCIDRs(list []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(list))
	for _, c := range list {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR: %s", c)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// BindHost returns "127.0.0.1" when every allowlisted CIDR is loopback (so the
// server stays local and no firewall prompt appears), or "0.0.0.0" when a
// non-loopback IP is allowed and LAN access is actually intended.
func BindHost(allowlist []string) string {
	for _, c := range allowlist {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		if !n.IP.IsLoopback() {
			return "0.0.0.0"
		}
	}
	return "127.0.0.1"
}

func ipAllowed(ip net.IP, nets []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// NormalizeCIDR validates user input and returns the canonical CIDR. A bare IP
// gets a host mask (/32 or /128) so the allowlist always carries a mask.
func NormalizeCIDR(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("enter an IP address")
	}
	if !strings.Contains(s, "/") {
		if strings.Contains(s, ":") {
			s += "/128"
		} else {
			s += "/32"
		}
	}
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return "", fmt.Errorf("invalid IP/mask: %s", s)
	}
	return ipnet.String(), nil
}

// OutboundIP returns the machine's primary LAN IP (no packets are sent).
func OutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr emits a structured error: {"error":{"code","message","status"}}.
// The stable `code` is what automated clients should branch on; `status` mirrors
// the HTTP status for convenience.
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
			"status":  status,
		},
	})
}
