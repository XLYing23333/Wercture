package main

import (
	"strings"
	"testing"
)

func TestFormatLink(t *testing.T) {
	const raw = "https://example.test/i/abc.png"
	cases := []struct {
		style, name, mime, want string
		fail                    bool
	}{
		{style: "", fail: true},
		{style: "url", name: "a.png", mime: "image/png", want: raw},
		{style: "typora", name: "a.png", mime: "image/png", want: raw},
		{style: "obsidian", name: "a.png", mime: "image/png", want: raw},
		{style: "markdown", name: "a.png", mime: "image/png", want: "![a.png](" + raw + ")"},
		{style: "markdown", name: "shot]\n.png", mime: "image/jpeg", want: "![shot .png](" + raw + ")"},
		{style: "markdown", name: "notes.pdf", mime: "application/pdf", want: "[notes.pdf](" + raw + ")"},
		{style: "markdown", name: "note.md", mime: "text/markdown", want: "[note.md](" + raw + ")"},
		{style: "html", name: "a.png", mime: "image/png", want: `<img src="` + raw + `" alt="a.png">`},
		{style: "html", name: `a<"&>.png`, mime: "image/webp", want: `<img src="` + raw + `" alt="a&lt;&quot;&amp;&gt;.png">`},
		{style: "html", name: "notes.pdf", mime: "application/pdf", want: `<a href="` + raw + `">notes.pdf</a>`},
		{style: "html", name: "note.md", mime: "text/markdown", want: `<a href="` + raw + `">note.md</a>`},
		{style: "picgo", fail: true},
	}
	for _, tc := range cases {
		got, err := formatLink(tc.style, raw, tc.name, tc.mime)
		if tc.fail {
			if err == nil {
				t.Fatalf("style %q accepted %q", tc.style, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("style %q: %v", tc.style, err)
		}
		if got != tc.want {
			t.Fatalf("style %q got %q want %q", tc.style, got, tc.want)
		}
	}
}

func TestLocaleFromSettings(t *testing.T) {
	if localeFromSettings(`{"locale":"zh-CN","theme":"system"}`) != "zh-CN" {
		t.Fatal("zh")
	}
	if localeFromSettings(`{"locale": "en", "theme": "light"}`) != "en" {
		t.Fatal("en")
	}
	if localeFromSettings("not json") != "en" {
		t.Fatal("fallback")
	}
	if texts("zh-CN").importURL != "导入 URL" || texts("en").importURL != "Import URL" {
		t.Fatal("menu language")
	}
}

func TestClipKeepsColor(t *testing.T) {
	line := switchItem("压缩", true, "33", "开", "关")
	if stripANSI(line) != "压缩  开" {
		t.Fatalf("plain %q", stripANSI(line))
	}
	clipped := clip(line, 80)
	if !strings.Contains(clipped, "\x1b[33m开") || !strings.Contains(clipped, "压缩") {
		t.Fatalf("clipped %q", clipped)
	}
}
