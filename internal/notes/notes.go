// Package notes holds the framework-agnostic core of go-Notepad: text
// statistics (the numbers a notepad shows in its status bar) and plain file
// load/save. It has no UI or Wails dependency, so it can be reused across apps
// and unit-tested in isolation — the same role a calc engine would play in a calculator.
package notes

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Stats describes a block of text the way a notepad status bar does: how many
// lines, words and characters it holds.
type Stats struct {
	Lines         int `json:"lines"`
	Words         int `json:"words"`
	Chars         int `json:"chars"`         // Unicode code points, whitespace included
	CharsNoSpaces int `json:"charsNoSpaces"` // characters excluding whitespace
}

// Compute returns the line, word and character counts for text, matching how a
// text editor reports them: an empty document is 1 line and 0 words, and every
// newline starts another line. Characters are counted as Unicode code points,
// so accents and emoji count as one each.
func Compute(text string) Stats {
	words := len(strings.Fields(text))
	noSpaces := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			noSpaces++
		}
	}
	return Stats{
		Lines:         1 + strings.Count(text, "\n"),
		Words:         words,
		Chars:         utf8.RuneCountInString(text),
		CharsNoSpaces: noSpaces,
	}
}

// Load reads a text file from disk and returns its contents. Keeping file I/O
// here (rather than in the Wails adapter) keeps the reusable core in charge of
// the logic; the adapter only supplies the path via a native dialog.
func Load(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Save writes content to path, creating the file if needed. 0644 matches a
// normal user-editable text file. The write is atomic (see WriteFileAtomic) so
// a crash mid-save never truncates the user's document.
func Save(path, content string) error {
	return WriteFileAtomic(path, []byte(content), 0o644)
}

// WriteFileAtomic writes data to path via a temp file in the same directory
// followed by a rename. os.WriteFile truncates first, so a crash mid-write
// would destroy the existing file; the rename swaps the complete new content
// in one step instead. An existing file keeps its permissions; a new file gets
// mode. The temp file lives in the target's directory (not the OS temp dir)
// because rename is only atomic within one filesystem.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Any failure below removes the temp file so aborted saves leave no litter.
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
