package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/media"
)

// Smaller than a product photo: the logo is paid for on every storefront page.
const kLogoMaxSize = 2 << 20

func (h *Handler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, kLogoMaxSize)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpjson.WriteBadRequest(w, "file required (max 2MB)")
		return
	}
	defer func() { _ = f.Close() }()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	// SVG is allowed here and nowhere else: a raster mark goes soft on retina.
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" &&
		ext != ".webp" && ext != ".svg" {
		httpjson.WriteBadRequest(w, "only jpeg/png/webp/svg")
		return
	}
	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	name := fmt.Sprintf("logo-%s%s", newToken()[:8], ext)
	dst, err := os.Create(filepath.Join(h.uploadsDir, name))
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, f); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	old := s.Logo
	s.Logo = name
	// The logo loads on every page; sellers upload 2000 px files for a 36 px header.
	if err := media.Shrink(h.uploadsDir, name); err != nil {
		log.Warnf("shrink logo %q: %v", name, err)
	}
	if err := h.db.UpdateSettings(s); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	removeLogoFile(h.uploadsDir, old)
	h.GetSettings(w, r)
}

func (h *Handler) DeleteLogo(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	old := s.Logo
	s.Logo = ""
	if err := h.db.UpdateSettings(s); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	removeLogoFile(h.uploadsDir, old)
	h.GetSettings(w, r)
}

// Best effort: a missing file must not fail the request.
func removeLogoFile(dir, name string) {
	if name == "" {
		return
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		log.Warnf("remove logo %q: %v", name, err)
	}
}
