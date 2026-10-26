package engine

import "testing"

func TestNormalizePanelURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                                   "",
		"coolify.example.com":                "http://coolify.example.com",
		"https://coolify.example.com/":       "https://coolify.example.com",
		"https://coolify.example.com:8443/x": "https://coolify.example.com:8443",
	} {
		got, err := NormalizePanelURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizePanelURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"ftp://x.com", "https://", "http:// spaces"} {
		if _, err := NormalizePanelURL(bad); err == nil {
			t.Errorf("NormalizePanelURL(%q) accepted", bad)
		}
	}
}

func TestResolvePanelURL(t *testing.T) {
	before := Panel{URL: "https://panel.new.example"}
	source := Panel{URL: "https://panel.old.example"}
	cases := map[string]string{
		"":                  "https://panel.new.example", // default keeps this server's address
		PanelKeep:           "https://panel.new.example",
		PanelSource:         "https://panel.old.example",
		PanelNone:           "",
		"-":                 "",
		"other.example.com": "http://other.example.com",
	}
	for choice, want := range cases {
		got, err := ResolvePanelURL(choice, before, source)
		if err != nil || got != want {
			t.Errorf("ResolvePanelURL(%q) = %q, %v; want %q", choice, got, err, want)
		}
	}
}

func TestPanelNote(t *testing.T) {
	src := Panel{URL: "https://old.example"}
	if n := panelNote(Panel{URL: "https://old.example"}, src); n == "" {
		t.Error("no note when the old server's address is used")
	}
	if n := panelNote(Panel{URL: "https://new.example"}, src); n == "" {
		t.Error("no note when this server keeps its own address")
	}
	if n := panelNote(Panel{}, Panel{}); n != "" {
		t.Errorf("note without addresses: %q", n)
	}
}
