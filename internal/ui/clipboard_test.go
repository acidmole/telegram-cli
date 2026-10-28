package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImageFile(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "cat.png")
	doc := filepath.Join(dir, "notes.txt")
	for _, p := range []string{img, doc} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for text, want := range map[string]string{
		img:                         img,
		"file://" + img + "\n":      img,
		doc:                         "",
		filepath.Join(dir, "x.png"): "",
		"hello there":               "",
		img + "\n" + img:            "",
	} {
		got := ""
		if a := imageFile(text); a != nil {
			got = a.path
		}
		if got != want {
			t.Errorf("imageFile(%q) = %q, want %q", text, got, want)
		}
	}
}
