// Package image converts and compresses still pictures with pure Go codecs.
package image

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/mayahiro/go-webp"
	xwebp "golang.org/x/image/webp"
)

// Mode selects local processing before upload.
type Mode string

const (
	// ModeNone uploads the original bytes.
	ModeNone Mode = "none"
	// ModeCompress re-encodes a still image at a quality setting.
	ModeCompress Mode = "compress"
	// ModeWebP encodes a still image as lossless WebP.
	ModeWebP Mode = "webp"
	// ModeBoth encodes a still image as lossy WebP at the quality setting.
	ModeBoth Mode = "both"
)

// Result is the bytes that should be uploaded.
type Result struct {
	Data []byte
	Name string
	MIME string
}

// Process applies mode. GIF and animated WebP are returned unchanged.
func Process(data []byte, name string, mode Mode, quality int) (Result, error) {
	if len(data) == 0 {
		return Result{}, errors.New("empty image")
	}
	if quality < 1 || quality > 100 {
		quality = 80
	}
	if isGIF(data) || isAnimatedWebP(data) {
		return Result{Data: data, Name: name, MIME: mimeOf(data)}, nil
	}
	kind := mimeOf(data)
	compress, toWebP := modeSwitches(mode)
	if (!compress && !toWebP) || kind == "" {
		if kind == "" {
			return Result{}, errors.New("unsupported image")
		}
		return Result{Data: data, Name: name, MIME: kind}, nil
	}
	img, err := decodeStill(data, kind)
	if err != nil {
		return Result{}, err
	}
	if toWebP {
		raw, err := encodeWebP(img, compress, quality)
		if err != nil {
			return Result{}, err
		}
		return Result{Data: raw, Name: replaceExt(name, ".webp"), MIME: "image/webp"}, nil
	}
	switch mode {
	case ModeCompress:
		var buf bytes.Buffer
		switch kind {
		case "image/jpeg":
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
				return Result{}, err
			}
			return Result{Data: buf.Bytes(), Name: name, MIME: kind}, nil
		case "image/png":
			if err := png.Encode(&buf, img); err != nil {
				return Result{}, err
			}
			return Result{Data: buf.Bytes(), Name: name, MIME: kind}, nil
		case "image/webp":
			if err := webp.Encode(&buf, img, &webp.Options{Compression: webp.CompressionLossy, Quality: quality}); err != nil {
				return Result{}, err
			}
			return Result{Data: buf.Bytes(), Name: name, MIME: kind}, nil
		default:
			return Result{Data: data, Name: name, MIME: kind}, nil
		}
	default:
		return Result{Data: data, Name: name, MIME: kind}, nil
	}
}

func modeSwitches(mode Mode) (compress, webp bool) {
	switch mode {
	case ModeCompress:
		return true, false
	case ModeWebP:
		return false, true
	case ModeBoth:
		return true, true
	default:
		return false, false
	}
}

func encodeWebP(img image.Image, lossy bool, quality int) ([]byte, error) {
	opt := &webp.Options{Compression: webp.CompressionLossless}
	if lossy {
		opt = &webp.Options{Compression: webp.CompressionLossy, Quality: quality}
	}
	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, opt); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeStill(data []byte, kind string) (image.Image, error) {
	if kind == "image/webp" {
		return xwebp.Decode(bytes.NewReader(data))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func mimeOf(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg"
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case isGIF(data):
		return "image/gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	default:
		return ""
	}
}

func isGIF(data []byte) bool {
	return len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a")))
}

func isAnimatedWebP(data []byte) bool {
	if len(data) < 12 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return false
	}
	for i := 12; i+8 <= len(data); {
		tag := string(data[i : i+4])
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if tag == "ANIM" || tag == "ANMF" {
			return true
		}
		i += 8 + size
		if size%2 == 1 {
			i++
		}
	}
	return false
}

func replaceExt(name, ext string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' || name[i] == '\\' {
			break
		}
		if name[i] == '.' {
			return name[:i] + ext
		}
	}
	return name + ext
}
