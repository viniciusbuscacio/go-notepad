package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxBinaryBytes caps how much we will extract from a downloaded archive, so a
// corrupted (or hostile) archive cannot fill the disk. Far above any real
// build of these apps.
const maxBinaryBytes = 512 << 20

// Install stages of interest to a UI; reported through the progress callback
// in this order.
const (
	StageDownloading = "downloading"
	StageVerifying   = "verifying"
	StageApplying    = "applying"
)

// Install downloads the release asset, verifies it against the release's
// checksums.txt, extracts the new binary and swaps it over the running
// executable. On any error the current executable is left untouched. It does
// NOT restart the app — call Relaunch, then exit, once this returns nil.
func (c *Client) Install(rel *Release, progress func(stage string)) error {
	step := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	if rel == nil || rel.AssetURL == "" {
		return fmt.Errorf("no downloadable build in this release")
	}
	if rel.ChecksumsURL == "" {
		return fmt.Errorf("release %s has no checksums.txt; refusing to install unverified binaries", rel.Tag)
	}
	exe, err := c.exePath()
	if err != nil {
		return fmt.Errorf("could not locate the running executable: %w", err)
	}

	step(StageDownloading)
	archive, err := c.downloadTemp(rel.AssetURL, "update-*"+archiveExt(rel.AssetName))
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(archive)

	step(StageVerifying)
	want, err := c.expectedChecksum(rel)
	if err != nil {
		return err
	}
	got, err := fileSHA256(archive)
	if err != nil {
		return err
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch for %s: the download is corrupt or tampered with", rel.AssetName)
	}

	step(StageApplying)
	// The new binary is staged NEXT TO the executable so the final rename never
	// crosses volumes (renames are only atomic within one).
	staged := exe + ".new"
	if err := extractBinary(archive, c.binaryName(), staged); err != nil {
		return fmt.Errorf("could not extract the new binary: %w", err)
	}
	if err := swap(exe, staged); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("could not replace the executable: %w", err)
	}
	return nil
}

// swap moves the running executable aside and the staged binary into its
// place. Windows forbids deleting a running exe but allows renaming it — the
// classic self-update dance. Rolls back if the second rename fails.
func swap(exe, staged string) error {
	old := exe + ".old"
	_ = os.Remove(old) // leftover from a previous update; ignore failures
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(staged, exe); err != nil {
		_ = os.Rename(old, exe) // roll back so the app still has its binary
		return err
	}
	return nil
}

// Relaunch starts the (already swapped) executable as a detached process.
// The caller should quit right after.
func (c *Client) Relaunch() error {
	exe, err := c.exePath()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}

// CleanupOld removes the ".old" binary a previous self-update left behind.
// Call it once at startup; errors are not interesting (on Windows the file may
// still be locked for a moment — the next launch gets it).
func (c *Client) CleanupOld() {
	if exe, err := c.exePath(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// downloadTemp streams url into a temp file and returns its path.
func (c *Client) downloadTemp(url, pattern string) (string, error) {
	resp, err := c.http_().Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server answered %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, maxBinaryBytes))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// expectedChecksum fetches checksums.txt and returns the hex SHA-256 recorded
// for the release's asset. Format: standard sha256sum output, one
// "<hex>  <filename>" per line.
func (c *Client) expectedChecksum(rel *Release) (string, error) {
	resp, err := c.http_().Get(rel.ChecksumsURL)
	if err != nil {
		return "", fmt.Errorf("could not download checksums.txt: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums.txt download answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == rel.AssetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", rel.AssetName)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func archiveExt(name string) string {
	if strings.HasSuffix(name, ".tar.gz") {
		return ".tar.gz"
	}
	return filepath.Ext(name)
}

// extractBinary pulls the entry whose base name matches binName out of a .zip
// or .tar.gz archive into dst (mode 0755). It matches by base name because
// each platform packages a different layout (bare exe, app/ dir, .app bundle).
func extractBinary(archive, binName, dst string) error {
	if strings.HasSuffix(archive, ".tar.gz") {
		return extractTarGz(archive, binName, dst)
	}
	return extractZip(archive, binName, dst)
}

func extractZip(archive, binName, dst string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != binName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeBinary(dst, rc)
	}
	return fmt.Errorf("%s not found in %s", binName, filepath.Base(archive))
}

func extractTarGz(archive, binName, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == binName {
			return writeBinary(dst, tr)
		}
	}
	return fmt.Errorf("%s not found in %s", binName, filepath.Base(archive))
}

func writeBinary(dst string, src io.Reader) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(src, maxBinaryBytes))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
