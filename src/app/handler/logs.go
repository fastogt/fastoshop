package handler

import (
	"net/http"
	"os"
)

type logInfoResponse struct {
	// Available is false when the shop was started without a log file: the admin
	// then says so instead of offering a link that answers 404.
	Available  bool   `json:"available"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

// LogInfo backs the expert block: whether there is a log at all, how big it is
// and when it last moved. Counting on the browser to discover that from a failed
// download would show the owner an error where an explanation belongs.
func (h *Handler) LogInfo(w http.ResponseWriter, r *http.Request) {
	if h.LogPath == "" {
		writeOK(w, logInfoResponse{})
		return
	}
	st, err := os.Stat(h.LogPath)
	if err != nil {
		writeOK(w, logInfoResponse{})
		return
	}
	writeOK(w, logInfoResponse{
		Available:  true,
		Size:       st.Size(),
		ModifiedAt: st.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// Logs hands the file over as plain text, the way the other services do. Whole
// file, not a tail: it is capped at ten megabytes, and paging through a log is a
// second tool, not a feature of this one.
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	if h.LogPath == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, h.LogPath)
}
