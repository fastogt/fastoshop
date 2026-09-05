package handler

import (
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type Handler struct {
	db         *database.Database
	uploadsDir string
	// OnStockChange wakes the marketplace sync; nil when the integration is off.
	OnStockChange func()
	// LogPath is the resolved log file; empty when the shop has none configured.
	LogPath string
	// job is the single background slot: nginx cuts a request off at sixty seconds.
	job job
	// login slows an attacker down: bcrypt alone allows ~1M guesses a night.
	login loginThrottle
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
