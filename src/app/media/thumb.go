// Package media makes the small copies the catalogue grid shows. A supplier's
// photo is 1000 px and 150 KB; a card is 220 px wide. Sixty of those on one page
// is two megabytes to draw thumbnails - the single heaviest thing about a shop
// with a real catalogue.
package media

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// kThumbWidth covers a 220 px card on a 2× screen. Height follows the aspect
// ratio: cards crop with object-fit, so a fixed height would only throw pixels
// away.
const kThumbWidth = 440

// kThumbQuality: 80 is where JPEG stops shrinking and starts smudging.
const kThumbQuality = 80

// ThumbName derives the small copy's name from the original instead of storing
// it: no column, no migration, and a photo deleted by name takes its thumbnail
// with it. Always .jpg - Go can encode JPEG and PNG, and a photo has no
// transparency to lose.
func ThumbName(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + ".t.jpg"
}

// HasThumb reports whether the small copy is on disk. Photos that predate
// thumbnails simply do not have one, and the storefront falls back to the
// original rather than showing a hole.
func HasThumb(dir, name string) bool {
	if name == "" || strings.HasPrefix(name, "http") {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, ThumbName(name)))
	return err == nil && st.Size() > 0
}

// MakeThumb writes the small copy next to the original. A failure is not fatal
// anywhere it is called: the shop keeps working off full-size photos, it is
// only slower.
func MakeThumb(dir, name string) error {
	return resize(filepath.Join(dir, name), filepath.Join(dir, ThumbName(name)), kThumbWidth)
}

// resize writes a JPEG copy of srcPath scaled down to width into dstPath (the
// two may be the same file). An image already narrow enough is left alone: a
// copy the size of the original would double the disk for nothing.
func resize(srcPath, dstPath string, width int) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	src, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(srcPath), err)
	}

	b := src.Bounds()
	if b.Dx() <= width {
		return nil
	}
	h := b.Dy() * width / b.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, width, h))
	// CatmullRom over ApproxBiLinear: this runs once per photo, and the result
	// is what every visitor sees on every catalogue page.
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	tmp := dstPath + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(out, dst, &jpeg.Options{Quality: kThumbQuality}); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Rename last: a half-written image must never be served, and the storefront
	// decides by the file's existence.
	return os.Rename(tmp, dstPath)
}

// RemoveThumb is best effort: deleting a photo should not fail because its
// small copy was already gone.
func RemoveThumb(dir, name string) {
	if name == "" || strings.HasPrefix(name, "http") {
		return
	}
	_ = os.Remove(filepath.Join(dir, ThumbName(name)))
}

// Missing lists the photos on disk that have no small copy yet: everything
// uploaded or downloaded before thumbnails existed. Walking the directory
// beats asking the database - the database does not know about thumbnails, the
// filesystem does.
func Missing(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".t.jpg") || strings.HasSuffix(name, ".part") {
			continue
		}
		// The shop logo lives in the same directory but is drawn at 36 px by the
		// header, never by the catalogue grid: it needs no small copy.
		if strings.HasPrefix(name, "logo-") {
			continue
		}
		if !HasThumb(dir, name) {
			out = append(out, name)
		}
	}
	return out, nil
}

// kThumbWorkers: making a thumbnail is CPU work, not waiting on a network, so
// the useful width is the number of cores rather than the eight the downloader
// uses. Four keeps a 1-core VPS responsive while the backfill runs.
const kThumbWorkers = 4

// MakeThumbs walks a list of photos and makes what is missing. The context
// stops it: a backfill over twenty thousand photos should be interruptible.
// Returns how many were made and how many failed.
func MakeThumbs(ctx context.Context, dir string, names []string, onProgress func(done int)) (int, int) {
	var (
		mu           sync.Mutex
		done, ok, ko int
	)
	queue := make(chan string)
	var wg sync.WaitGroup
	for range kThumbWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range queue {
				err := MakeThumb(dir, name)
				mu.Lock()
				done++
				if err != nil {
					ko++
					log.Warnf("thumbnail for %q: %v", name, err)
				} else {
					ok++
				}
				if onProgress != nil {
					onProgress(done)
				}
				mu.Unlock()
			}
		}()
	}
	for _, n := range names {
		select {
		case queue <- n:
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return ok, ko
		}
	}
	close(queue)
	wg.Wait()
	return ok, ko
}

// kLogoWidth: the header draws the logo 36 px tall, and a 2× screen with a wide
// wordmark needs no more than this. Sellers upload what their designer gave
// them, which is routinely a 2000 px, 100 KB file loaded on every single page.
const kLogoWidth = 440

// Shrink replaces a raster image with a narrower copy of itself, in place.
// Vector logos and images already small enough are left alone.
func Shrink(dir, name string) error {
	if strings.EqualFold(filepath.Ext(name), ".svg") {
		return nil
	}
	full := filepath.Join(dir, name)
	return resize(full, full, kLogoWidth)
}
