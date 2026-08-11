package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/fastogt/fastoshop/app/database"
)

const kSessionTTL = 30 * 24 * time.Hour

// Приглашение живёт сутки: его пересылают письмом, и просроченная ссылка
// безопаснее забытой действующей.
const kInviteTTL = 24 * time.Hour

const kPurposeInvite = "invite"

type setupStatusResponse struct {
	Needed bool `json:"needed"`
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type inviteRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type okStatusResponse struct {
	Status string `json:"status"`
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// isTLS — прод стоит за nginx+certbot, который терминирует TLS и проксирует
// плейн-HTTP, поэтому r.TLS там всегда nil: полагаемся на X-Forwarded-Proto.
// Без этого Secure-кука выключила бы вход в проде, а на локальном http — вход
// вообще, поэтому флаг не константа.
func isTLS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (h *Handler) setSession(w http.ResponseWriter, r *http.Request) error {
	tok := newToken()
	if err := h.db.CreateToken(tok, "session", time.Now().Add(kSessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: "session", Value: tok, Path: "/",
		HttpOnly: true, Secure: isTLS(r), SameSite: http.SameSiteLaxMode,
		MaxAge: int(kSessionTTL.Seconds()),
	})
	return nil
}

func (h *Handler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	_, err := h.db.GetSettings()
	writeOK(w, setupStatusResponse{Needed: err != nil})
}

func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db.GetSettings(); err == nil {
		http.NotFound(w, r)
		return
	}
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Email == "" || len(req.Password) < 8 {
		writeBadRequest(w, "email and password (min 8 chars) required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: req.Email, PasswordHash: string(hash)}); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.setSession(w, r); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "created"})
}

// Invite закрывает провижининг без пересылки пароля: владелец заводится при
// создании магазина, а пароль задаёт сам по одноразовой ссылке. Токен сгорает
// при использовании, поэтому перехваченное письмо через сутки бесполезно.
func (h *Handler) Invite(w http.ResponseWriter, r *http.Request) {
	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Password) < 8 {
		writeBadRequest(w, "token and password (min 8 chars) required")
		return
	}
	if !h.db.ValidToken(req.Token, kPurposeInvite) {
		writeUnauthorized(w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.db.SetOwnerPassword(string(hash)); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.db.UseToken(req.Token); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.setSession(w, r); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "ok"})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	s, err := h.db.GetSettings()
	if err != nil || s.OwnerEmail != req.Email ||
		bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(req.Password)) != nil {
		writeUnauthorized(w)
		return
	}
	if err := h.setSession(w, r); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "ok"})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	if len(req.NewPassword) < 8 {
		writeBadRequest(w, "new password must be at least 8 characters")
		return
	}
	s, err := h.db.GetSettings()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(req.CurrentPassword)) != nil {
		writeUnauthorized(w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.PasswordHash = string(hash)
	if err := h.db.UpdateSettings(s); err != nil {
		writeInternalError(w, err)
		return
	}
	// Кука уже проверена SessionAuth, поэтому она точно есть.
	if c, err := r.Cookie("session"); err == nil {
		if err := h.db.DeleteOtherTokens(c.Value); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	writeOK(w, okStatusResponse{Status: "ok"})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		_ = h.db.UseToken(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "session", Value: "", Path: "/",
		HttpOnly: true, Secure: isTLS(r), SameSite: http.SameSiteLaxMode,
		MaxAge: -1,
	})
	writeOK(w, okStatusResponse{Status: "ok"})
}

func (h *Handler) SessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil || !h.db.ValidToken(c.Value, "session") {
			writeUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
