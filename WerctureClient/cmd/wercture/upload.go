package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"wercture/client/internal/filekind"
	"wercture/client/internal/image"
)

type uploadSource struct {
	Name, MIME string
	Reader     io.Reader
	Close      func() error
}

func uploadKind(pdf, md, video, audio bool) (string, error) {
	kind, count := filekind.Image, 0
	for name, enabled := range map[string]bool{filekind.PDF: pdf, filekind.Markdown: md, filekind.Video: video, filekind.Audio: audio} {
		if enabled {
			kind = name
			count++
		}
	}
	if count > 1 {
		return "", errors.New("--pdf, --md, --video and --audio are mutually exclusive")
	}
	return kind, nil
}

func openUpload(name, expected string, mode image.Mode, quality int) (result uploadSource, err error) {
	f, err := os.Open(name)
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return result, errors.New("upload needs a nonempty regular file")
	}
	header := make([]byte, 4096)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return result, err
	}
	header = header[:n]
	base := filepath.Base(name)
	ext := strings.ToLower(filepath.Ext(base))
	actual := filekind.Classify(base, header)
	// A Markdown preview may start with a long blank section.
	if actual == "" && (ext == ".md" || ext == ".markdown" || ext == ".mdown") {
		actual = filekind.Markdown
	}
	if actual != expected {
		if actual == filekind.Video || actual == filekind.Audio {
			return result, fmt.Errorf("%s files need upload --%s", actual, actual)
		}
		if actual == filekind.PDF {
			return result, errors.New("pdf files need upload --pdf")
		}
		if actual == filekind.Markdown {
			return result, errors.New("markdown files need upload --md")
		}
		return result, fmt.Errorf("not a supported %s file", expected)
	}
	if actual == filekind.Markdown {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return result, err
		}
		reader := bufio.NewReader(f)
		content := false
		for {
			value, size, err := reader.ReadRune()
			if err == io.EOF {
				break
			}
			if err != nil {
				return result, err
			}
			if value == 0 || (value == utf8.RuneError && size == 1) {
				return result, errors.New("not a UTF-8 Markdown file")
			}
			if !unicode.IsSpace(value) {
				content = true
			}
		}
		if !content {
			return result, errors.New("empty Markdown file")
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	result = uploadSource{Name: base, Reader: f, Close: f.Close}
	switch actual {
	case filekind.Audio, filekind.Video:
		result.MIME = filekind.MediaMIME(base, header)
	case filekind.PDF:
		result.MIME = "application/pdf"
	case filekind.Markdown:
		result.MIME = "text/markdown"
	case filekind.Image:
		if mode == image.ModeNone || bytes.HasPrefix(header, []byte("GIF")) || (len(header) >= 21 && bytes.Equal(header[12:16], []byte("VP8X")) && header[20]&2 != 0) {
			switch {
			case bytes.HasPrefix(header, []byte{0xff, 0xd8, 0xff}):
				result.MIME = "image/jpeg"
			case bytes.HasPrefix(header, []byte("\x89PNG")):
				result.MIME = "image/png"
			case bytes.HasPrefix(header, []byte("GIF")):
				result.MIME = "image/gif"
			default:
				result.MIME = "image/webp"
			}
		} else {
			data, err := io.ReadAll(f)
			if err != nil {
				return uploadSource{}, err
			}
			processed, err := image.Process(data, base, mode, quality)
			if err != nil {
				return uploadSource{}, err
			}
			result.Name, result.MIME, result.Reader = processed.Name, processed.MIME, bytes.NewReader(processed.Data)
		}
	}
	return result, nil
}

func postSource(client *http.Client, endpoint, token string, file uploadSource) (string, error) {
	reader, pipe := io.Pipe()
	defer reader.Close()
	writer := multipart.NewWriter(pipe)
	go func() {
		part, err := writer.CreateFormFile("file", file.Name)
		if err == nil {
			_, err = io.Copy(part, file.Reader)
		}
		if err == nil {
			err = writer.Close()
		}
		_ = pipe.CloseWithError(err)
	}()
	req, err := http.NewRequest(http.MethodPost, endpoint, reader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload status %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	url := jsonString(raw, "url")
	if file.MIME == "text/markdown" {
		if render := jsonString(raw, "render_url"); render != "" {
			url = render
		}
	}
	if url == "" {
		return "", errors.New("upload response has no url")
	}
	return url, nil
}
