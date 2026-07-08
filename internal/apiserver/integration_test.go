package apiserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestServerEndToEnd(t *testing.T) {
	// The injected op mirrors the real one: return line/word/char counts.
	s := New(func(text string) (any, error) {
		return map[string]int{"words": len(strings.Fields(text))}, nil
	}, nil, nil)
	cfg := Config{Port: 18737, Key: "secret", Allowlist: []string{"127.0.0.1/32"}}
	if err := s.Start(cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	base := "http://127.0.0.1:18737"

	// Authenticated stats.
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/stats", strings.NewReader(`{"text":"ola mundo"}`))
	req.Header.Set("X-API-Key", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stats request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d, quer 200", resp.StatusCode)
	}
	var out struct {
		Words int `json:"words"`
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if out.Words != 2 {
		t.Errorf("words = %d, quer 2", out.Words)
	}

	// Missing key is rejected.
	noKey, _ := http.NewRequest(http.MethodGet, base+"/v1/health", nil)
	r2, err := http.DefaultClient.Do(noKey)
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-key status = %d, quer 401", r2.StatusCode)
	}

	// Health with key.
	ok, _ := http.NewRequest(http.MethodGet, base+"/v1/health", nil)
	ok.Header.Set("X-API-Key", "secret")
	r3, err := http.DefaultClient.Do(ok)
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	r3.Body.Close()
	if r3.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, quer 200", r3.StatusCode)
	}
}
