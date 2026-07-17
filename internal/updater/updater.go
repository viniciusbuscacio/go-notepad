// Package updater checks GitHub Releases for a newer build and swaps the
// running executable in place. It is framework-agnostic (no Wails) and knows
// nothing about UI: the host app decides when to check, what to show and when
// to install. Reusable across the app family — inject the repo/app names.
package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the real GitHub API. Tests (and a smoke stub) inject a
// local server instead.
const DefaultBaseURL = "https://api.github.com"

// Client locates and applies releases for one app.
type Client struct {
	BaseURL string // GitHub API base; DefaultBaseURL when empty
	Repo    string // e.g. "viniciusbuscacio/go-notepad"
	App     string // binary/asset base name, e.g. "go-notepad"
	ExePath string // running executable; resolved via os.Executable when empty (injectable for tests)
	HTTP    *http.Client
}

// Release describes the newest published version and the asset for this
// platform.
type Release struct {
	Version      string `json:"version"` // "0.2.0" (no leading v)
	Tag          string `json:"tag"`     // "v0.2.0"
	Notes        string `json:"notes"`   // release body, shown as plain text
	AssetName    string `json:"assetName"`
	AssetURL     string `json:"assetUrl"`
	ChecksumsURL string `json:"checksumsUrl"` // "" when the release has no checksums.txt
}

func (c *Client) http_() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (c *Client) exePath() (string, error) {
	if c.ExePath != "" {
		return c.ExePath, nil
	}
	return os.Executable()
}

// githubRelease is the slice of the GitHub API response we need.
type githubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check fetches the latest release and reports whether it is newer than
// current. The Release is returned even when it is not newer, so callers can
// show "you're up to date (X)".
func (c *Client) Check(current string) (*Release, bool, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.base(), c.Repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.App)
	resp, err := c.http_().Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("could not reach the update server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("update server answered %d", resp.StatusCode)
	}
	var gh githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return nil, false, fmt.Errorf("unexpected response from the update server: %w", err)
	}
	if gh.TagName == "" {
		return nil, false, fmt.Errorf("the latest release has no tag")
	}

	rel := &Release{
		Tag:     gh.TagName,
		Version: strings.TrimPrefix(gh.TagName, "v"),
		Notes:   gh.Body,
	}
	wantAsset := c.assetName(gh.TagName)
	for _, a := range gh.Assets {
		switch a.Name {
		case wantAsset:
			rel.AssetName, rel.AssetURL = a.Name, a.URL
		case "checksums.txt":
			rel.ChecksumsURL = a.URL
		}
	}
	newer := compareVersions(rel.Version, current) > 0
	if newer && rel.AssetURL == "" {
		return rel, false, fmt.Errorf("release %s has no build for %s/%s", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	return rel, newer, nil
}

// assetName reconstructs the deterministic asset name release.yml publishes:
// <app>-<tag>-<os>-<arch>.<zip|tar.gz>, with "darwin" spelled "macos".
func (c *Client) assetName(tag string) string {
	osToken := runtime.GOOS
	ext := ".tar.gz"
	switch runtime.GOOS {
	case "windows":
		ext = ".zip"
	case "darwin":
		osToken, ext = "macos", ".zip"
	}
	return fmt.Sprintf("%s-%s-%s-%s%s", c.App, tag, osToken, runtime.GOARCH, ext)
}

// binaryName is the file to pull out of the downloaded archive.
func (c *Client) binaryName() string {
	if runtime.GOOS == "windows" {
		return c.App + ".exe"
	}
	return c.App
}

// compareVersions compares two dotted versions ("0.2.1", with or without a
// leading v). Anything unparseable — including the "dev" default of an
// untagged build — counts as 0, so every real release is newer than a dev
// build. Returns -1, 0 or 1.
func compareVersions(a, b string) int {
	av, bv := parseVersion(a), parseVersion(b)
	for i := range av {
		if av[i] != bv[i] {
			if av[i] > bv[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func parseVersion(s string) [3]int {
	var out [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	// Drop any pre-release/build suffix ("0.2.0-rc1" -> "0.2.0").
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	for i, part := range strings.SplitN(s, ".", 3) {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}
		}
		out[i] = n
	}
	return out
}
