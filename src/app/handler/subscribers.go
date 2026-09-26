package handler

import (
	"encoding/csv"
	"net/http"
	"strconv"

	"github.com/fastogt/fastoshop/app/httpjson"
)

type subscribersResponse struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

// Subscribers is the count the profile shows next to the export: the owner needs
// to know whether there is a list at all before downloading it.
func (h *Handler) Subscribers(w http.ResponseWriter, r *http.Request) {
	active, total, err := h.db.SubscriberCount()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, subscribersResponse{Active: active, Total: total})
}

// ExportSubscribersCSV carries the proof with the addresses: a mailing service
// asks for the date and the wording of the consent, not just the list.
func (h *Handler) ExportSubscribersCSV(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.Subscribers()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="subscribers.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "email", "source", "subscribed_at", "unsubscribed_at", "consent"})
	for _, s := range list {
		off := ""
		if s.UnsubscribedAt.Valid {
			off = s.UnsubscribedAt.Time.Format("2006-01-02 15:04")
		}
		_ = cw.Write([]string{
			strconv.FormatInt(s.ID, 10), s.Email, s.Source,
			s.CreatedAt.Format("2006-01-02 15:04"), off, s.ConsentText,
		})
	}
	cw.Flush()
}
