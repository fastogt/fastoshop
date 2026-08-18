package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// The link check is the fix for a catalogue that must stay hotlinked: it drops
// what the supplier deleted and copies nothing.
func TestCheckImages(t *testing.T) {
	srv := imageServer(t)
	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()

	id := seedProduct(t, d, "Одеяло", "Ромашка",
		srv.URL+"/ok.png", srv.URL+"/gone.jpg", srv.URL+"/down.jpg")
	imgs, _ := d.ListRemoteImages(database.Selection{All: true, Supplier: "Ромашка"})

	alive, gone, failed := CheckImages(context.Background(), d, imgs, nil)
	if alive != 1 || gone != 1 || failed != 1 {
		t.Fatalf("alive=%d gone=%d failed=%d", alive, gone, failed)
	}
	got, _ := d.ListImages(id)
	if len(got) != 2 {
		t.Fatalf("only the deleted one goes: %+v", got)
	}
	for _, im := range got {
		if strings.HasSuffix(im.Path, "/gone.jpg") {
			t.Fatalf("a photo the supplier deleted must lose its row: %+v", got)
		}
		// Nothing is downloaded: the rows that survive still point at the supplier.
		if !strings.HasPrefix(im.Path, "http") {
			t.Fatalf("the check must not copy anything: %+v", got)
		}
	}
}
