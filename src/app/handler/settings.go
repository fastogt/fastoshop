package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
	"github.com/fastogt/fastoshop/app/mail"
)

type settingsResponse struct {
	OwnerEmail      string `json:"owner_email"`
	ShopName        string `json:"shop_name"`
	ShopPhone       string `json:"shop_phone"`
	Currency        string `json:"currency"`
	Lang            string `json:"lang"`
	Logo            string `json:"logo"`
	SMTPHost        string `json:"smtp_host"`
	SMTPPort        int    `json:"smtp_port"`
	SMTPUser        string `json:"smtp_user"`
	SMTPPasswordSet bool   `json:"smtp_password_set"`

	GAMeasurementID  string `json:"ga_measurement_id"`
	MetrikaCounterID string `json:"metrika_counter_id"`
	Requisites       string `json:"requisites"`
}

type settingsRequest struct {
	ShopName     string  `json:"shop_name"`
	ShopPhone    string  `json:"shop_phone"`
	Currency     string  `json:"currency"`
	Lang         string  `json:"lang"`
	SMTPHost     string  `json:"smtp_host"`
	SMTPPort     int     `json:"smtp_port"`
	SMTPUser     string  `json:"smtp_user"`
	SMTPPassword *string `json:"smtp_password"` // nil = leave unchanged

	// Pointers: an older admin build does not send these fields, and a missing
	// field must leave the counters alone instead of tearing them off the page.
	GAMeasurementID  *string `json:"ga_measurement_id"`
	MetrikaCounterID *string `json:"metrika_counter_id"`
	Requisites       *string `json:"requisites"`
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeOK(w, settingsResponse{
		OwnerEmail: s.OwnerEmail, ShopName: s.ShopName, ShopPhone: s.ShopPhone,
		Currency: s.Currency, Lang: s.Lang, Logo: s.Logo,
		SMTPHost: s.SMTPHost, SMTPPort: s.SMTPPort, SMTPUser: s.SMTPUser,
		SMTPPasswordSet:  s.SMTPPassword != "",
		GAMeasurementID:  s.GAMeasurementID,
		MetrikaCounterID: s.MetrikaCounterID,
		Requisites:       s.Requisites,
	})
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	s.ShopName, s.ShopPhone = req.ShopName, req.ShopPhone
	// An empty currency means an older admin build that does not send the field:
	// keep what is stored instead of failing validation.
	if req.Currency != "" {
		if !database.IsValidShopCurrency(req.Currency) {
			writeBadRequest(w, h.msg(i18n.KeyBadCurrency))
			return
		}
		s.Currency = req.Currency
	}
	if req.Lang != "" {
		s.Lang = req.Lang
	}
	s.SMTPHost, s.SMTPPort, s.SMTPUser = req.SMTPHost, req.SMTPPort, req.SMTPUser
	if req.SMTPPassword != nil {
		s.SMTPPassword = *req.SMTPPassword
	}
	if req.GAMeasurementID != nil {
		s.GAMeasurementID = strings.TrimSpace(*req.GAMeasurementID)
	}
	if req.MetrikaCounterID != nil {
		s.MetrikaCounterID = strings.TrimSpace(*req.MetrikaCounterID)
	}
	if req.Requisites != nil {
		s.Requisites = strings.TrimSpace(*req.Requisites)
	}
	if err := h.db.UpdateSettings(s); err != nil {
		writeInternalError(w, err)
		return
	}
	h.GetSettings(w, r)
}

type langRequest struct {
	Lang string `json:"lang"`
}

// SetLang is its own endpoint rather than a field of the settings form: the
// language switch lives in the header and must not resubmit — and so overwrite —
// whatever the owner happens to be editing in the profile at that moment.
func (h *Handler) SetLang(w http.ResponseWriter, r *http.Request) {
	var req langRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !i18n.IsValidLang(req.Lang) {
		writeBadRequest(w, "lang must be ru or en")
		return
	}
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.Lang = req.Lang
	if err := h.db.UpdateSettings(s); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "ok"})
}

// TestSMTP sends a test email to oneself — the "Check" button in the profile.
func (h *Handler) TestSMTP(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetSettings()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := mail.Send(s, i18n.T(s.Lang, i18n.KeyTestMailSubject),
		i18n.T(s.Lang, i18n.KeyTestMailBody)); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	writeOK(w, okStatusResponse{Status: "sent"})
}
