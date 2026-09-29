package handler

import (
	"archive/zip"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
	"github.com/fastogt/fastoshop/app/importer"
)

// Supplier photos are fetched on the fly and never stored: filling the catalogue is "Fill in".
func (h *Handler) ExportProductPhotos(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	p, err := h.db.GetProduct(id)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}
	imgs, err := h.db.ListImages(id)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if len(imgs) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNoPhotos))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+archiveName(p.Slug)+`"`)
	zw := zip.NewWriter(w)
	var missed []string
	for i, im := range imgs {
		data, ext, err := h.photoBytes(im.Path)
		if err != nil {
			// A short archive that says nothing about the gap is worse than one that names it.
			missed = append(missed, fmt.Sprintf("%02d - %s - %v", i+1, im.Path, err))
			log.Warnf("photo %q of product %d: %v", im.Path, id, err)
			continue
		}
		f, err := zw.Create(fmt.Sprintf("%02d%s", i+1, ext))
		if err != nil {
			log.Warnf("zip entry for product %d: %v", id, err)
			return
		}
		if _, err := f.Write(data); err != nil {
			log.Warnf("zip write for product %d: %v", id, err)
			return
		}
	}
	if len(missed) > 0 {
		if f, err := zw.Create("missing.txt"); err == nil {
			_, _ = f.Write([]byte(h.msg(i18n.KeyPhotoFailed) + "\n\n" + strings.Join(missed, "\n") + "\n"))
		}
	}
	if err := zw.Close(); err != nil {
		log.Warnf("closing the archive for product %d: %v", id, err)
	}
}

func (h *Handler) photoBytes(path string) ([]byte, string, error) {
	if strings.HasPrefix(path, "http") {
		return importer.FetchImageBytes(path)
	}
	// Base only: a path from the database must never climb out of its directory.
	data, err := os.ReadFile(filepath.Join(h.uploadsDir, filepath.Base(path)))
	if err != nil {
		return nil, "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		ext = ".jpg"
	}
	return data, ext, nil
}

// Content-Disposition with Cyrillic arrives mangled or nameless in some browsers.
func archiveName(slug string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(slug) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteRune(c)
		}
	}
	name := strings.Trim(b.String(), "-_")
	if name == "" {
		name = "product"
	}
	return name + "-photos.zip"
}
