package importer

import (
	"context"
	"errors"
	"net/http"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// CheckImages asks the supplier whether each hotlinked photo is still there and
// drops the rows for those that are gone, without downloading anything.
//
// "Fill in photos" already does this on the way past, but it also copies every
// picture onto our disk — sixty thousand files, and a copy of somebody else's
// material is a different conversation from a link to it. When the catalogue
// must stay hotlinked, this is the only way to stop a dead link from rendering
// as the product's title sprawled across the tile.
//
// HEAD, not GET: the answer we need is the status code, and a shop of twenty
// thousand photos should not pull gigabytes to learn it. A server that refuses
// HEAD simply yields no deletions — silence is not evidence of death.
func CheckImages(ctx context.Context, db *database.Database, imgs []database.ProductImage,
	onProgress func(done int)) (alive, gone, failed int) {
	var (
		mu   sync.Mutex
		done int
	)

	queue := make(chan database.ProductImage)
	var wg sync.WaitGroup
	for range kImageWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for im := range queue {
				err := headImage(im.Path)
				dead := errors.Is(err, ErrImageGone)
				if dead {
					if derr := db.DeleteImage(im.ID); derr != nil {
						log.Warnf("drop gone image %q: %v", im.Path, derr)
						dead = false
					}
				}
				if err != nil && !dead {
					log.Warnf("check image %q: %v", im.Path, err)
				}

				mu.Lock()
				done++
				switch {
				case dead:
					gone++
				case err != nil:
					failed++
				default:
					alive++
				}
				if onProgress != nil {
					onProgress(done)
				}
				mu.Unlock()
			}
		}()
	}
	for _, im := range imgs {
		select {
		case queue <- im:
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return alive, gone, failed
		}
	}
	close(queue)
	wg.Wait()
	return alive, gone, failed
}

func headImage(url string) error {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return err
	}
	resp, err := kHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return imageStatusError(resp.StatusCode)
}
