package image

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"testing"
)

func TestGIFIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), buf.Bytes()...)
	out, err := Process(original, "move.gif", ModeWebP, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Data, original) || out.MIME != "image/gif" || out.Name != "move.gif" {
		t.Fatalf("gif changed %#v", out.Name)
	}
}

func TestJPEGCompressKeepsSizeAndDropsMetadata(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 10), B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	out, err := Process(buf.Bytes(), "photo.jpg", ModeCompress, 40)
	if err != nil {
		t.Fatal(err)
	}
	if out.MIME != "image/jpeg" || out.Name != "photo.jpg" {
		t.Fatalf("kind %s %s", out.MIME, out.Name)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(out.Data))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 24 {
		t.Fatalf("size %v", decoded.Bounds())
	}
	if bytes.Contains(out.Data, []byte("Exif")) {
		t.Fatal("exif survived")
	}
}

func TestJPEGToWebP(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.SetRGBA(1, 1, color.RGBA{R: 200, G: 10, B: 10, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	out, err := Process(buf.Bytes(), "photo.jpg", ModeWebP, 80)
	if err != nil {
		t.Fatal(err)
	}
	if out.MIME != "image/webp" || out.Name != "photo.webp" || !bytes.Contains(out.Data, []byte("VP8L")) {
		t.Fatalf("webp %#v", out)
	}
	both, err := Process(buf.Bytes(), "photo.jpg", ModeBoth, 40)
	if err != nil {
		t.Fatal(err)
	}
	if both.MIME != "image/webp" || both.Name != "photo.webp" || !bytes.Contains(both.Data, []byte("VP8 ")) {
		t.Fatalf("both %#v", both)
	}
}
