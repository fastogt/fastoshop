package handler

import (
	"github.com/fastogt/fastoshop/app/config"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type Handler struct {
	cfg        *config.Config
	db         *database.Database
	uploadsDir string
	// OnStockChange будит синк площадок. Поле, а не аргумент конструктора:
	// зависимость односторонняя и необязательная (в тестах и при выключенной
	// интеграции её просто нет), а тащить её через сигнатуру пришлось бы всем.
	OnStockChange func()
	// job is the single background slot: the import and the photo download both
	// outlive a request, and nginx cuts one off at sixty seconds.
	job job
}

func NewHandler(cfg *config.Config, db *database.Database, uploadsDir string) *Handler {
	return &Handler{cfg: cfg, db: db, uploadsDir: uploadsDir}
}

func (h *Handler) stockChanged() {
	if h.OnStockChange != nil {
		h.OnStockChange()
	}
}

// lang reports the owner's language. A broken settings row must not swallow the
// error text, so it falls back to the default rather than propagating.
func (h *Handler) lang() string {
	s, err := h.db.GetSettings()
	if err != nil {
		return i18n.LangRU
	}
	return s.Lang
}

func (h *Handler) msg(key string) string { return i18n.T(h.lang(), key) }
