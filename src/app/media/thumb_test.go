package media

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func writeJPEG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestThumbName(t *testing.T) {
	for in, want := range map[string]string{
		"p1-abc.jpg":  "p1-abc.t.jpg",
		"p1-abc.png":  "p1-abc.t.jpg",
		"p1-abc.webp": "p1-abc.t.jpg",
	} {
		if got := ThumbName(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

// The whole point: a supplier's full-size photo must not be what a 220 px card
// downloads.
func TestMakeThumbShrinks(t *testing.T) {
	dir := t.TempDir()
	name := writeJPEG(t, dir, "p1-abc.jpg", 1200, 900)
	big, _ := os.Stat(filepath.Join(dir, name))

	if err := MakeThumb(dir, name); err != nil {
		t.Fatal(err)
	}
	if !HasThumb(dir, name) {
		t.Fatal("thumbnail not created")
	}
	small, err := os.Stat(filepath.Join(dir, ThumbName(name)))
	if err != nil {
		t.Fatal(err)
	}
	if small.Size() >= big.Size() {
		t.Fatalf("thumbnail %d bytes is not smaller than the original %d", small.Size(), big.Size())
	}

	f, _ := os.Open(filepath.Join(dir, ThumbName(name)))
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != kThumbWidth {
		t.Fatalf("width %d, want %d", cfg.Width, kThumbWidth)
	}
	// Aspect ratio kept: a squashed photo is worse than a heavy one.
	if want := 900 * kThumbWidth / 1200; cfg.Height != want {
		t.Fatalf("height %d, want %d", cfg.Height, want)
	}
}

// A photo already smaller than a card gets no copy: it would double the disk
// for nothing.
func TestMakeThumbSkipsSmall(t *testing.T) {
	dir := t.TempDir()
	name := writeJPEG(t, dir, "p2-abc.jpg", 200, 200)
	if err := MakeThumb(dir, name); err != nil {
		t.Fatal(err)
	}
	if HasThumb(dir, name) {
		t.Fatal("a small photo must not get a thumbnail")
	}
}

// Remote photos live on the supplier's server: there is nothing of ours to
// shrink, and the storefront has to fall back to the link.
func TestRemoteHasNoThumb(t *testing.T) {
	if HasThumb(t.TempDir(), "https://cdn.example/x.jpg") {
		t.Fatal("a link cannot have a local thumbnail")
	}
}

func TestMissingAndBackfill(t *testing.T) {
	dir := t.TempDir()
	writeJPEG(t, dir, "p1-a.jpg", 1000, 800)
	writeJPEG(t, dir, "p2-b.jpg", 1000, 800)
	writeJPEG(t, dir, "logo-c.jpg", 1000, 800)

	missing, err := Missing(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The logo is drawn at 36 px by the header and never by the grid.
	if len(missing) != 2 {
		t.Fatalf("missing: %v", missing)
	}

	ok, failed := MakeThumbs(context.Background(), dir, missing, nil)
	if ok != 2 || failed != 0 {
		t.Fatalf("ok=%d failed=%d", ok, failed)
	}
	// Second pass has nothing left to do — the action is repeatable.
	again, _ := Missing(dir)
	if len(again) != 0 {
		t.Fatalf("still missing after a full pass: %v", again)
	}
}

// A photo deleted from the admin must take its small copy with it, otherwise
// the uploads directory grows with files nothing points at.
func TestRemoveThumb(t *testing.T) {
	dir := t.TempDir()
	name := writeJPEG(t, dir, "p1-a.jpg", 1000, 800)
	if err := MakeThumb(dir, name); err != nil {
		t.Fatal(err)
	}
	RemoveThumb(dir, name)
	if HasThumb(dir, name) {
		t.Fatal("thumbnail outlived its photo")
	}
	RemoveThumb(dir, name) // twice must not panic
}
