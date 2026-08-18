package handler

import (
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type Handler struct {
	db         *database.Database
	uploadsDir string
	// OnStockChange wakes the marketplace sync. A field, not a constructor
	// argument: the dependency is one-way and optional (in tests and with the
	// integration disabled it simply isn't there), while threading it through
	// the signature would burden every caller.
	OnStockChange func()
	// LogPath is the resolved log file. A field for the same reason: the admin
	// serves it, but a shop with no log file configured works exactly as before.
	LogPath string
	// job is the single background slot: the import and the photo download both
	// outlive a request, and nginx cuts one off at sixty seconds.
	job job
}

func NewHandler(db *database.Database, uploadsDir string) *Handler {
	return &Handler{db: db, uploadsDir: uploadsDir}
}

func (h *Handler) stockChanged() {
	if h.OnStockChange != nil {
		h.OnStockChange()
	}
}

func (h *Handler) msg(key string) string { return i18n.T(h.db.Lang(), key) }
