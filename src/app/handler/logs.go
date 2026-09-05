package handler

import (
	"net/http"
	"os"
	"time"

	"github.com/fastogt/fastoshop/app/httpjson"
)

type logInfoResponse struct {
	// Available is false when the shop was started without a log file.
	Available  bool   `json:"available"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

func (h *Handler) LogInfo(w http.ResponseWriter, r *http.Request) {
	if h.LogPath == "" {
		httpjson.WriteOK(w, logInfoResponse{})
		return
	}
	st, err := os.Stat(h.LogPath)
	if err != nil {
		httpjson.WriteOK(w, logInfoResponse{})
		return
	}
	httpjson.WriteOK(w, logInfoResponse{
		Available:  true,
		Size:       st.Size(),
		ModifiedAt: st.ModTime().UTC().Format(time.RFC3339),
	})
}

// The whole file, not a tail: it is capped at ten megabytes.
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	if h.LogPath == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, h.LogPath)
}
