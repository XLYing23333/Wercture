package preview

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderPDF(t *testing.T) {
	stream := []byte("BT /F1 48 Tf 72 200 Td (Hello PDF) Tj ET\n")
	data := buildSample(stream)
	img, err := renderPDF(data)
	if err != nil {
		t.Fatal(err)
	}
	dark := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if c.R < 200 || c.G < 200 || c.B < 200 {
				dark++
			}
		}
	}
	if dark < 200 {
		t.Fatalf("pdf page looks blank: %d dark pixels, bounds %v", dark, b)
	}
}

func buildSample(stream []byte) []byte {
	objs := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n",
		"4 0 obj\n<< /Length " + itoa(len(stream)) + " >>\nstream\n" + string(stream) + "endstream\nendobj\n",
		"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
	}
	var body []byte
	body = append(body, []byte("%PDF-1.4\n")...)
	offsets := make([]int, len(objs))
	for i, obj := range objs {
		offsets[i] = len(body)
		body = append(body, obj...)
	}
	start := len(body)
	body = append(body, []byte("xref\n0 6\n0000000000 65535 f \n")...)
	for _, off := range offsets {
		body = append(body, []byte(pad10(off)+" 00000 n \n")...)
	}
	body = append(body, []byte("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n")...)
	body = append(body, []byte(itoa(start)+"\n%%EOF\n")...)
	return body
}

func TestChinesePreview(t *testing.T) {
	matches, err := filepath.Glob("../webui/dist/assets/ComicNeueCN-Regular-*.otf")
	if err != nil || len(matches) == 0 {
		t.Fatal(err, matches)
	}
	face, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	img := textPage([]string{"中文预览"}, face)
	dark := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if c.R < 200 || c.G < 200 || c.B < 200 {
				dark++
			}
		}
	}
	if dark < 40 {
		t.Fatalf("chinese preview looks blank: %d", dark)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func pad10(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}
