package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.1.0", 1},
		{"0.1.0", "0.2.0", -1},
		{"0.1.1", "0.1.1", 0},
		{"v0.2.0", "0.2.0", 0},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1}, // numeric, not lexicographic
		{"0.2.0", "dev", 1},    // dev build counts as 0.0.0
		{"dev", "0.0.1", -1},
		{"0.2.0-rc1", "0.2.0", 0}, // pre-release suffix is ignored
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	c := &Client{App: "go-notepad"}
	name := c.assetName("v0.2.0")
	if !strings.HasPrefix(name, "go-notepad-v0.2.0-") {
		t.Fatalf("assetName = %q, want go-notepad-v0.2.0-<os>-<arch>", name)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(name, "-macos-") {
		t.Errorf("assetName on darwin = %q, want the 'macos' token", name)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".zip") {
		t.Errorf("assetName on windows = %q, want .zip", name)
	}
	if runtime.GOOS == "linux" && !strings.HasSuffix(name, ".tar.gz") {
		t.Errorf("assetName on linux = %q, want .tar.gz", name)
	}
}

// fakeRelease builds a stub GitHub server: /repos/<repo>/releases/latest plus
// the asset and checksums downloads, all self-hosted. binContent becomes the
// platform archive's binary entry; tamper flips the recorded checksum.
func fakeRelease(t *testing.T, tag string, binContent []byte, tamper bool) *httptest.Server {
	t.Helper()
	c := &Client{App: "go-notepad"}
	assetName := c.assetName(tag)
	binName := c.binaryName()

	var archive []byte
	if strings.HasSuffix(assetName, ".tar.gz") {
		archive = makeTarGz(t, "go-notepad/"+binName, binContent)
	} else {
		archive = makeZip(t, binName, binContent)
	}
	sum := sha256.Sum256(archive)
	checksum := hex.EncodeToString(sum[:])
	if tamper {
		checksum = strings.Repeat("0", 64)
	}
	checksums := fmt.Sprintf("%s  %s\n", checksum, assetName)

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/user/go-notepad/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"body":     "release notes for " + tag,
			"assets": []map[string]string{
				{"name": assetName, "browser_download_url": srv.URL + "/dl/" + assetName},
				{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
			},
		})
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func makeZip(t *testing.T, entry string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTarGz(t *testing.T, entry string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: entry, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testClient(srv *httptest.Server, exe string) *Client {
	return &Client{
		BaseURL: srv.URL,
		Repo:    "user/go-notepad",
		App:     "go-notepad",
		ExePath: exe,
	}
}

func TestCheckFindsNewerRelease(t *testing.T) {
	srv := fakeRelease(t, "v0.2.0", []byte("new binary"), false)
	c := testClient(srv, "")

	rel, newer, err := c.Check("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !newer {
		t.Fatal("expected v0.2.0 to be newer than 0.1.0")
	}
	if rel.Version != "0.2.0" || rel.Tag != "v0.2.0" {
		t.Errorf("release = %+v", rel)
	}
	if rel.AssetURL == "" || rel.ChecksumsURL == "" {
		t.Errorf("asset/checksums URL missing: %+v", rel)
	}
	if rel.Notes == "" {
		t.Error("release notes missing")
	}
}

func TestCheckUpToDate(t *testing.T) {
	srv := fakeRelease(t, "v0.2.0", []byte("bin"), false)
	c := testClient(srv, "")

	rel, newer, err := c.Check("0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if newer {
		t.Fatal("same version must not be reported as newer")
	}
	if rel.Version != "0.2.0" {
		t.Errorf("release = %+v", rel)
	}
}

func TestCheckServerDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	c := testClient(srv, "")
	if _, _, err := c.Check("0.1.0"); err == nil {
		t.Fatal("expected an error when the server is unreachable")
	}
}

func TestInstallSwapsBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go-notepad")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := fakeRelease(t, "v0.2.0", []byte("new binary"), false)
	c := testClient(srv, exe)
	rel, _, err := c.Check("0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	var stages []string
	if err := c.Install(rel, func(s string) { stages = append(stages, s) }); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Errorf("executable content = %q, want the new binary", got)
	}
	old, err := os.ReadFile(exe + ".old")
	if err != nil {
		t.Fatal("expected the old binary to be parked at .old:", err)
	}
	if string(old) != "old binary" {
		t.Errorf(".old content = %q, want the old binary", old)
	}
	want := []string{StageDownloading, StageVerifying, StageApplying}
	if fmt.Sprint(stages) != fmt.Sprint(want) {
		t.Errorf("stages = %v, want %v", stages, want)
	}

	// CleanupOld removes the parked binary.
	c.CleanupOld()
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Error("CleanupOld left the .old binary behind")
	}
}

func TestInstallRejectsChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go-notepad")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := fakeRelease(t, "v0.2.0", []byte("evil binary"), true)
	c := testClient(srv, exe)
	rel, _, err := c.Check("0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	err = c.Install(rel, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected a checksum mismatch error, got %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "old binary" {
		t.Error("the executable must remain untouched after a failed install")
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Error("no staged .new binary may be left behind")
	}
}

func TestInstallRefusesWithoutChecksums(t *testing.T) {
	c := &Client{App: "go-notepad"}
	err := c.Install(&Release{Tag: "v9.9.9", AssetURL: "http://127.0.0.1:1/x.zip"}, nil)
	if err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Fatalf("expected a refusal without checksums.txt, got %v", err)
	}
}
