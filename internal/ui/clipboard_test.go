package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMediaFile(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "cat.png")
	vid := filepath.Join(dir, "clip.MP4")
	doc := filepath.Join(dir, "notes.txt")
	for _, p := range []string{img, vid, doc} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for text, want := range map[string]string{
		img:                         img,
		vid:                         vid,
		"file://" + img + "\n":      img,
		doc:                         "",
		filepath.Join(dir, "x.png"): "",
		"hello there":               "",
		img + "\n" + img:            "",
	} {
		got := ""
		if a := mediaFile(text); a != nil {
			got = a.path
		}
		if got != want {
			t.Errorf("mediaFile(%q) = %q, want %q", text, got, want)
		}
	}
}
