package mail

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// FetchPicture reads a message's remote picture: an image, of 8 MB at
// most, sent without cookies or a referrer.
func FetchPicture(ctx context.Context, src string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	if t := resp.Header.Get("Content-Type"); t != "" && !strings.HasPrefix(t, "image/") && !strings.HasPrefix(t, "application/octet-stream") {
		return nil, errors.New("not an image: " + t)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 8<<20 {
		return nil, errors.New("too large")
	}
	return b, nil
}

// ScalePicture decodes a picture and scales it down to width pixels at
// most (its own width when that is less), its premultiplied RGBA pixels.
func ScalePicture(data []byte, width int) (*image.RGBA, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, errors.New("empty picture")
	}
	w := min(width, b.Dx())
	if w <= 0 {
		w = b.Dx()
	}
	h := max(b.Dy()*w/b.Dx(), 1)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	} else {
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	}
	return dst, nil
}
