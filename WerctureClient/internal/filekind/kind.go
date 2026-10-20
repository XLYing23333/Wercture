// Package filekind tells an upload apart as an image, PDF, or Markdown file.
package filekind

import (
	"bytes"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	// PDF is a document that starts with the PDF header.
	PDF = "pdf"
	// Markdown is a text file with a Markdown extension.
	Markdown = "markdown"
	// Image is a JPEG, PNG, GIF, or WebP.
	Image = "image"
)

// Classify inspects the file name and bytes.
func Classify(name string, data []byte) string {
	switch {
	case isJPEG(data), isPNG(data), isGIF(data), isWebP(data):
		return Image
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return PDF
	case markdownName(name) && len(bytes.TrimSpace(data)) > 0 && utf8.Valid(data) && !bytes.Contains(data, []byte{0}):
		return Markdown
	default:
		return ""
	}
}

func markdownName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown":
		return true
	default:
		return false
	}
}

func isJPEG(data []byte) bool {
	return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
}

func isPNG(data []byte) bool {
	return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
}

func isGIF(data []byte) bool {
	return len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a")))
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
}
