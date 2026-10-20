// Package preview draws the gallery picture for a PDF or Markdown file.
package preview

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	pageW = 900
	pageH = 1200
)

var writeMu sync.Mutex

// Write stores a PNG beside the document. An existing preview is left as it is.
func Write(path, mime string, data, fontBytes []byte) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	img := page(mime, data, fontBytes)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return writeAtomic(path, buf.Bytes())
}

func page(mime string, data, fontBytes []byte) image.Image {
	if mime == "application/pdf" {
		if img, err := renderPDF(data); err == nil && img != nil {
			return fit(img)
		}
		return textPage([]string{"PDF"}, fontBytes)
	}
	lines := markdownLines(string(data))
	if len(lines) == 0 {
		lines = []string{"Markdown"}
	}
	return textPage(lines, fontBytes)
}

func fit(src image.Image) image.Image {
	b := src.Bounds()
	if b.Dx() == pageW && b.Dy() == pageH && b.Min.X == 0 && b.Min.Y == 0 {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	// Top-align a page that is already close to the target size.
	x := (pageW - b.Dx()) / 2
	if x < 0 {
		x = 0
	}
	draw.Draw(dst, image.Rect(x, 0, x+b.Dx(), b.Dy()), src, b.Min, draw.Over)
	return dst
}

var (
	faceOnce  sync.Once
	titleFace font.Face
	bodyFace  font.Face
)

func faces(fontBytes []byte) (font.Face, font.Face) {
	faceOnce.Do(func() {
		src := fontBytes
		parsed, err := opentype.Parse(src)
		if err != nil || len(src) == 0 {
			parsed, err = opentype.Parse(goregular.TTF)
		}
		if err != nil {
			return
		}
		titleFace, _ = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 34, DPI: 72, Hinting: font.HintingFull})
		bodyFace, _ = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 22, DPI: 72, Hinting: font.HintingFull})
	})
	return titleFace, bodyFace
}

func textPage(lines []string, fontBytes []byte) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	border := color.RGBA{220, 224, 228, 255}
	for x := 0; x < pageW; x++ {
		img.SetRGBA(x, 0, border)
		img.SetRGBA(x, pageH-1, border)
	}
	for y := 0; y < pageH; y++ {
		img.SetRGBA(0, y, border)
		img.SetRGBA(pageW-1, y, border)
	}
	title, body := faces(fontBytes)
	if title == nil || body == nil {
		return img
	}
	ink := image.NewUniform(color.RGBA{31, 35, 40, 255})
	y := 96
	maxWidth := fixed.I(pageW - 128)
	heading := true
	for _, line := range lines {
		if y > pageH-72 {
			break
		}
		if strings.TrimSpace(line) == "" {
			y += 18
			continue
		}
		face := body
		if heading {
			face = title
			heading = false
		}
		for _, part := range wrap(face, line, maxWidth) {
			if y > pageH-72 {
				break
			}
			d := &font.Drawer{Dst: img, Src: ink, Face: face, Dot: fixed.P(64, y)}
			d.DrawString(part)
			y += face.Metrics().Height.Ceil() + 10
		}
		if face == title {
			y += 12
		}
	}
	return img
}

func wrap(face font.Face, text string, maxWidth fixed.Int26_6) []string {
	var out []string
	var b strings.Builder
	var width fixed.Int26_6
	for _, r := range text {
		adv, ok := face.GlyphAdvance(r)
		if !ok {
			adv = fixed.I(face.Metrics().Height.Ceil() / 2)
		}
		if b.Len() > 0 && width+adv > maxWidth {
			out = append(out, b.String())
			b.Reset()
			width = 0
		}
		b.WriteRune(r)
		width += adv
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func markdownLines(src string) []string {
	if len(src) > 12000 {
		src = src[:12000]
		for !utf8.ValidString(src) && len(src) > 0 {
			src = src[:len(src)-1]
		}
	}
	var lines []string
	fence := false
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			fence = !fence
			continue
		}
		if strings.HasPrefix(trim, "|") && strings.Contains(trim, "---") {
			continue
		}
		trim = strings.TrimLeft(trim, "#> ")
		trim = strings.Trim(trim, "|")
		trim = strings.ReplaceAll(trim, "|", "  ")
		trim = strings.ReplaceAll(trim, "**", "")
		trim = strings.ReplaceAll(trim, "__", "")
		trim = strings.ReplaceAll(trim, "`", "")
		trim = strings.ReplaceAll(trim, "$", "")
		lines = append(lines, strings.TrimSpace(trim))
		if len(lines) > 48 {
			break
		}
	}
	return lines
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
