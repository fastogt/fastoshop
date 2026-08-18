package importer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/media"
)

// Same ceiling the admin upload uses: a photo bigger than this is a mistake at
// the supplier's end, not a product picture.
const kMaxImageBytes = 10 << 20

// ErrImageGone marks a photo the supplier has removed for good. Such a row is
// deleted instead of retried forever: an imported photo is a link, the link is
// the only copy, and a catalogue page renders a dead link as the alt text
// sprawling where the picture should be. Without the row the storefront falls
// back to its own "no photo" placeholder.
var ErrImageGone = errors.New("image gone")

// imageStatusError reads a status the way both the download and the link check
// must read it.
//
// 404 and 410 only: those two say the supplier removed the file, and the link
// will never work again. A 403 is hotlink protection, a 429 is our own eight
// workers, a 5xx is their bad hour — all of them come back, and a body served as
// HTML under a 200 is caught by the content sniff rather than treated as a
// death. Widening this list would erase live photos during somebody else's
// outage, and there is no copy to restore them from.
func imageStatusError(code int) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusNotFound, http.StatusGone:
		return fmt.Errorf("http %d: %w", code, ErrImageGone)
	}
	return fmt.Errorf("http %d", code)
}

// ponytail: eight at a time is what turns 60 000 photos from a day into an
// hour without looking like a crawl to the supplier. Make it a setting when
// someone's host starts refusing us.
const kImageWorkers = 8

// localName mirrors the admin upload naming (p<id>-<token><ext>) so an imported
// photo and an uploaded one are indistinguishable on disk and in the DB.
func localName(productID int64, ext string) (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("p%d-%s%s", productID, hex.EncodeToString(raw), ext), nil
}

// extByContent trusts the bytes, not the URL: suppliers serve .jpg links that
// answer with an HTML error page, and saving that as a photo would leave a
// broken card behind with no way to tell it apart from a real one.
func extByContent(head []byte) string {
	switch http.DetectContentType(head) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// SaveBlob writes a picture that arrived inside a source file and returns its
// local name. The naming, the size ceiling and the thumbnail are the same as for
// a downloaded photo: on disk an imported picture must be indistinguishable from
// an uploaded one.
func SaveBlob(productID int64, blob []byte, uploadsDir string) (string, error) {
	if uploadsDir == "" {
		return "", fmt.Errorf("no uploads directory")
	}
	if len(blob) > kMaxImageBytes {
		return "", fmt.Errorf("larger than %d MB", kMaxImageBytes>>20)
	}
	ext := extByContent(blob)
	if ext == "" {
		return "", fmt.Errorf("not a jpeg, png or webp")
	}
	name, err := localName(productID, ext)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, name), blob, 0644); err != nil {
		return "", err
	}
	if err := media.MakeThumb(uploadsDir, name); err != nil {
		log.Warnf("thumbnail for %q: %v", name, err)
	}
	return name, nil
}

func fetchImage(im database.ProductImage, uploadsDir string) (string, error) {
	resp, err := kHTTP.Get(im.Path)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := imageStatusError(resp.StatusCode); err != nil {
		return "", err
	}
	// One byte over the cap is enough to tell "too big" from "exactly at the cap".
	data, err := io.ReadAll(io.LimitReader(resp.Body, kMaxImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > kMaxImageBytes {
		return "", fmt.Errorf("larger than %d MB", kMaxImageBytes>>20)
	}
	ext := extByContent(data)
	if ext == "" {
		return "", fmt.Errorf("not a jpeg, png or webp")
	}
	name, err := localName(im.ProductID, ext)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, name), data, 0644); err != nil {
		return "", err
	}
	// The small copy is made here, where the file has just been written: sixty
	// full-size photos on one catalogue page are megabytes of traffic.
	if err := media.MakeThumb(uploadsDir, name); err != nil {
		log.Warnf("thumbnail for %q: %v", name, err)
	}
	return name, nil
}

// LocalizeImages downloads photos that still live on the supplier's server and
// points the catalogue at our own copies. A photo that fails to download keeps
// its link: a shop with a hotlinked picture beats a shop with no picture. A
// photo the supplier has deleted loses its row instead — see ErrImageGone.
//
// The context stops the run: it is the way out of a download of sixty thousand
// photos started by mistake.
//
// onProgress is called after every photo with the number finished and the ids
// being downloaded right now — that is what the admin draws its spinner from.
func LocalizeImages(ctx context.Context, db *database.Database, uploadsDir string,
	imgs []database.ProductImage, onProgress func(done int, inFlight []int64)) (ok, gone, failed int) {
	var (
		mu       sync.Mutex
		done     int
		inFlight = map[int64]int{}
	)
	report := func() {
		ids := make([]int64, 0, len(inFlight))
		for id := range inFlight {
			ids = append(ids, id)
		}
		if onProgress != nil {
			onProgress(done, ids)
		}
	}

	queue := make(chan database.ProductImage)
	var wg sync.WaitGroup
	for range kImageWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for im := range queue {
				mu.Lock()
				inFlight[im.ProductID]++
				report()
				mu.Unlock()

				name, err := fetchImage(im, uploadsDir)
				if err == nil {
					if err = db.SetImagePath(im.ID, name); err != nil {
						// The file is on disk but nothing points at it — remove it
						// rather than leave the uploads dir growing invisibly.
						_ = os.Remove(filepath.Join(uploadsDir, name))
					}
				}
				dead := errors.Is(err, ErrImageGone)
				if dead {
					if derr := db.DeleteImage(im.ID); derr != nil {
						log.Warnf("drop gone image %q: %v", im.Path, derr)
						dead = false
					}
				}
				if err != nil {
					log.Warnf("localize image %q: %v", im.Path, err)
				}

				mu.Lock()
				done++
				switch {
				case err == nil:
					ok++
				case dead:
					gone++
				default:
					failed++
				}
				if inFlight[im.ProductID]--; inFlight[im.ProductID] <= 0 {
					delete(inFlight, im.ProductID)
				}
				report()
				mu.Unlock()
			}
		}()
	}
	// Stopping only closes the tap: a photo already being downloaded is cheaper
	// to finish than to throw away.
	for _, im := range imgs {
		select {
		case queue <- im:
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return ok, gone, failed
		}
	}
	close(queue)
	wg.Wait()
	return ok, gone, failed
}
