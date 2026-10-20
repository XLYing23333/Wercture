package preview

import (
	"errors"
	"image"
	"image/draw"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

var (
	poolOnce sync.Once
	pool     pdfium.Pool
	poolErr  error
)

func renderPDF(data []byte) (image.Image, error) {
	poolOnce.Do(func() {
		pool, poolErr = webassembly.Init(webassembly.Config{
			MinIdle:  1,
			MaxIdle:  1,
			MaxTotal: 1,
		})
	})
	if poolErr != nil {
		return nil, poolErr
	}
	instance, err := pool.GetInstance(30 * time.Second)
	if err != nil {
		return nil, err
	}
	defer instance.Close()
	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, err
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	rendered, err := instance.RenderPageInPixels(&requests.RenderPageInPixels{
		Width:  pageW,
		Height: pageH,
		Page: requests.Page{ByIndex: &requests.PageByIndex{
			Document: doc.Document,
			Index:    0,
		}},
	})
	if err != nil {
		return nil, err
	}
	defer rendered.Cleanup()
	src := rendered.Result.Image
	if src == nil {
		return nil, errors.New("empty pdf page")
	}
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst, nil
}
