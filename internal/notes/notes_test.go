package notes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompute(t *testing.T) {
	cases := []struct {
		name                          string
		in                            string
		lines, words, chars, noSpaces int
	}{
		{"vazio", "", 1, 0, 0, 0},
		{"uma palavra", "ola", 1, 1, 3, 3},
		{"duas palavras", "ola mundo", 1, 2, 9, 8},
		{"duas linhas", "ola\nmundo", 2, 2, 9, 8},
		{"linha final vazia", "ola\n", 2, 1, 4, 3},
		{"espacos extras", "  a   b  ", 1, 2, 9, 2},
		{"acentos contam como um", "cão", 1, 1, 3, 3},
		{"tabs e quebras", "a\tb\nc", 2, 3, 5, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(tc.in)
			if got.Lines != tc.lines || got.Words != tc.words ||
				got.Chars != tc.chars || got.CharsNoSpaces != tc.noSpaces {
				t.Errorf("Compute(%q) = %+v; quer lines=%d words=%d chars=%d noSpaces=%d",
					tc.in, got, tc.lines, tc.words, tc.chars, tc.noSpaces)
			}
		})
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nota.txt")
	const content = "linha 1\nlinha 2\ncão 🚀"

	if err := Save(path, content); err != nil {
		t.Fatalf("Save erro: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load erro: %v", err)
	}
	if got != content {
		t.Errorf("round-trip = %q, quer %q", got, content)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nao-existe.txt")); err == nil {
		t.Error("Load de arquivo inexistente esperava erro, obteve nil")
	}
}

func TestSaveOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nota.txt")
	if err := Save(path, "primeiro"); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, "segundo"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "segundo" {
		t.Errorf("Save did not overwrite: %q", b)
	}
}
