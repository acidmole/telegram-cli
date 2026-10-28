package telegram

import "testing"

func TestShortName(t *testing.T) {
	for name, want := range map[string]string{
		"cat.png":                 "cat.png",
		"exactly14c.jpg":          "exactly14c.jpg",
		"holiday-beach-2026.jpg":  "holiday-b~.jpg",
		"scan.thisisaverylongext": "scan.thisisav~",
	} {
		got := ShortName(name)
		if got != want {
			t.Errorf("ShortName(%q) = %q, want %q", name, got, want)
		}
		if n := len([]rune(got)); n > 14 {
			t.Errorf("ShortName(%q) is %d characters", name, n)
		}
	}
}
