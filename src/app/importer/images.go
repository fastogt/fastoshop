package importer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// Same ceiling the admin upload uses.
const kMaxImageBytes = 10 << 20

// ponytail: eight downloads at a time, fast without crawling the supplier.
// Make it a setting when someone's host starts refusing us.
const kImageWorkers = 8

// localName mirrors the admin upload naming (p<id>-<token><ext>).
func localName(productID int64, ext string) (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("p%d-%s%s", productID, hex.EncodeToString(raw), ext), nil
}

// Trust the bytes, not the URL: suppliers serve .jpg links that answer with HTML.
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

func fetchImage(im database.ProductImage, uploadsDir string) (string, error) {
	resp, err := kHTTP.Get(im.Path)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
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
	if err := media.MakeThumb(uploadsDir, name); err != nil {
		log.Warnf("thumbnail for %q: %v", name, err)
	}
	return name, nil
}

// LocalizeImages copies supplier photos to our disk; a failed one keeps its link.
func LocalizeImages(ctx context.Context, db *database.Database, uploadsDir string,
	imgs []database.ProductImage, onProgress func(done int, inFlight []int64)) (ok, failed int) {
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
						// Nothing points at the file now: do not leave it in uploads.
						_ = os.Remove(filepath.Join(uploadsDir, name))
					}
				}
				if err != nil {
					log.Warnf("localize image %q: %v", im.Path, err)
				}

				mu.Lock()
				done++
				if err == nil {
					ok++
				} else {
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
	// Stopping only closes the tap; photos already in flight are finished.
	for _, im := range imgs {
		select {
		case queue <- im:
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return ok, failed
		}
	}
	close(queue)
	wg.Wait()
	return ok, failed
}
