package apiserver

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"strings"
	"testing"
)

// TestServerTLSAndPin verifies the server serves HTTPS with its self-signed
// cert, that a trusting client reaches it, and that the public-key pin is stable
// across restarts (proving the key is persisted, not regenerated each time).
func TestServerTLSAndPin(t *testing.T) {
	dir := t.TempDir()
	op := func(string) (any, error) { return map[string]int{"words": 1}, nil }

	s := New(op, nil, nil)
	if err := s.Start(Config{
		Port:      0, // OS picks a free port
		Key:       "secret",
		Allowlist: []string{"127.0.0.1/32"},
		TLS:       true,
		CertDir:   dir,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Stop()

	if s.Fingerprint() == "" {
		t.Fatal("expected a non-empty public-key pin when TLS is on")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(s.CertPEM()) {
		t.Fatal("could not load the server certificate")
	}
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}}
	req, _ := http.NewRequest(http.MethodPost, "https://"+s.Addr()+"/v1/stats",
		strings.NewReader(`{"text":"seis por sete"}`))
	req.Header.Set("X-API-Key", "secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("https request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	pin := s.Fingerprint()
	_ = s.Stop()

	// Same CertDir → same persisted key → same pin, even though the certificate
	// itself is freshly minted on this second start.
	s2 := New(op, nil, nil)
	if err := s2.Start(Config{Port: 0, Key: "secret", Allowlist: []string{"127.0.0.1/32"}, TLS: true, CertDir: dir}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer s2.Stop()
	if s2.Fingerprint() != pin {
		t.Errorf("pin changed across restart: got %q, want %q", s2.Fingerprint(), pin)
	}
}
