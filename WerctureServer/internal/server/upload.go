package server

import (
	"bufio"
	"context"
	"errors"
	"image"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"wercture/server/internal/preview"
	"wercture/server/internal/store"
)

const documentBudget = 32 << 20
const markdownPreviewBudget = 256 << 10

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func receiveUpload(r *http.Request, file *os.File, limit int64) (string, int64, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" {
		return "", 0, errBadType
	}
	reader := multipart.NewReader(io.LimitReader(contextReader{r.Context(), r.Body}, limit+(1<<20)), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err != nil {
			return "", 0, errBadType
		}
		if part.FormName() != "file" {
			continue
		}
		n, err := io.Copy(file, io.LimitReader(part, limit+1))
		if n > limit {
			return "", n, errTooLarge
		}
		if err != nil {
			return "", n, err
		}
		if n == 0 {
			return "", 0, errBadType
		}
		return part.FileName(), n, r.Context().Err()
	}
}

func validateMarkdown(ctx context.Context, file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(contextReader{ctx, file})
	content := false
	for {
		r, size, err := reader.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if r == 0 || (r == utf8.RuneError && size == 1) {
			return errBadType
		}
		if !unicode.IsSpace(r) {
			content = true
		}
	}
	if !content {
		return errBadType
	}
	return nil
}

func (s *Server) acceptUpload(r *http.Request, warehouseID, uploaderID int64) (store.Image, error) {
	defer r.Body.Close()
	settings, err := s.store.GetSettings()
	if err != nil {
		return store.Image{}, err
	}
	dir := filepath.Join(s.store.DataDir(), "files")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return store.Image{}, err
	}
	file, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return store.Image{}, err
	}
	staged := file.Name()
	committed := false
	defer func() { file.Close(); removeStored(staged) }()
	name, size, err := receiveUpload(r, file, settings.MaxUploadBytes)
	if err != nil {
		return store.Image{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return store.Image{}, err
	}
	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return store.Image{}, err
	}
	header = header[:n]
	kind, ok := sniff(header, name)
	width, height := 0, 0
	ext := strings.ToLower(filepath.Ext(name))
	if strings.HasPrefix(kind, "image/") {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return store.Image{}, err
		}
		cfg, _, err := image.DecodeConfig(file)
		if err != nil || cfg.Width < 1 || cfg.Height < 1 {
			return store.Image{}, errBadType
		}
		width, height = cfg.Width, cfg.Height
	} else if kind == "application/pdf" {
		width, height = 900, 1200
	} else if ext == ".md" || ext == ".markdown" || ext == ".mdown" {
		if err := validateMarkdown(r.Context(), file); err != nil {
			return store.Image{}, err
		}
		kind, ok, width, height = "text/markdown", true, 900, 1200
	} else {
		kind, width, height, err = s.inspectMedia(r.Context(), staged, name, header)
		if err != nil {
			return store.Image{}, err
		}
		ok = true
	}
	if !ok {
		return store.Image{}, errBadType
	}
	if err := file.Close(); err != nil {
		return store.Image{}, err
	}
	if err := s.filePreview(staged, kind); err != nil {
		return store.Image{}, err
	}
	if err := r.Context().Err(); err != nil {
		return store.Image{}, err
	}
	publicID, err := newPublicID()
	if err != nil {
		return store.Image{}, err
	}
	rel := filepath.Join("files", publicID)
	abs := filepath.Join(s.store.DataDir(), rel)
	defer func() {
		if !committed {
			removeStored(abs)
		}
	}()
	if err := os.Rename(staged, abs); err != nil {
		return store.Image{}, err
	}
	if _, err := os.Stat(staged + ".preview.png"); err == nil {
		if err := os.Rename(staged+".preview.png", abs+".preview.png"); err != nil {
			return store.Image{}, err
		}
	}
	item, err := s.store.InsertImage(store.NewImage{PublicID: publicID, Name: cleanName(name), MIME: kind, Size: size, Width: width, Height: height, StoragePath: rel, WarehouseID: warehouseID, UploaderID: uploaderID})
	if err != nil {
		return store.Image{}, err
	}
	committed = true
	return item, nil
}

// filePreview bounds document reads; video previews are generated during validation.
func (s *Server) filePreview(path, kind string) error {
	if kind != "application/pdf" && kind != "text/markdown" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	var data []byte
	if kind == "application/pdf" {
		if info.Size() <= documentBudget {
			data, err = io.ReadAll(io.LimitReader(f, documentBudget+1))
		}
	} else {
		data, err = io.ReadAll(io.LimitReader(f, markdownPreviewBudget))
	}
	if err != nil {
		return err
	}
	return preview.Write(path+".preview.png", kind, data, documentFont())
}

func copyStored(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
