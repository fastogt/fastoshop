package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// kLogoMaxSize is smaller than a product photo on purpose: a header logo that
// weighs a megabyte is paid for on every page of the storefront, by every
// visitor, on mobile data.
const kLogoMaxSize = 2 << 20

// UploadLogo replaces the shop's logo. One file, not a gallery: a shop has one
// mark, and the previous file goes with it so the uploads folder does not
// collect every attempt.
func (h *Handler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, kLogoMaxSize)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeBadRequest(w, "file required (max 2MB)")
		return
	}
	defer func() { _ = f.Close() }()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	// SVG is allowed here and nowhere else: a logo is drawn by a designer at one
	// size and shown at another, and a raster mark goes soft on retina screens.
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" &&
		ext != ".webp" && ext != ".svg" {
		writeBadRequest(w, "only jpeg/png/webp/svg")
		return
	}
	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		writeInternalError(w, err)
		return
	}
	name := fmt.Sprintf("logo-%s%s", newToken()[:8], ext)
	dst, err := os.Create(filepath.Join(h.uploadsDir, name))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, f); err != nil {
		writeInternalError(w, err)
		return
	}
	old := s.Logo
	s.Logo = name
	if err := h.db.UpdateSettings(s); err != nil {
		writeInternalError(w, err)
		return
	}
	removeLogoFile(h.uploadsDir, old)
	h.GetSettings(w, r)
}

// DeleteLogo puts the shop back to showing its name, which is what a storefront
// without a logo should do — an empty header would be worse than plain text.
func (h *Handler) DeleteLogo(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	old := s.Logo
	s.Logo = ""
	if err := h.db.UpdateSettings(s); err != nil {
		writeInternalError(w, err)
		return
	}
	removeLogoFile(h.uploadsDir, old)
	h.GetSettings(w, r)
}

// removeLogoFile is best effort: a missing file must not fail the request, but
// leaving every replaced logo behind would grow the volume for nothing.
func removeLogoFile(dir, name string) {
	if name == "" {
		return
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		log.Warnf("remove logo %q: %v", name, err)
	}
}
