package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
)

const kSessionTTL = 30 * 24 * time.Hour

// Login is not rate-limited by refusing: a shop has one owner and nobody to
// call, so a lockout after N tries would let anyone who knows their address
// shut them out of their own admin until somebody reaches the server over SSH.
// A growing delay costs an attacker time and costs the owner nothing - they
// type the right password and the counter resets.
//
// ponytail: one counter for the whole instance rather than a bucket per
// address. There is one owner, so "somebody is guessing" is a single fact, and
// per-IP state would need expiry while an attacker rotates addresses anyway.
// Per-IP buckets are the upgrade if a shop ever has more than one login.
const (
	kLoginFreeTries = 3
	kMaxLoginDelay  = 8 * time.Second
)

type loginThrottle struct {
	mu     sync.Mutex
	failed int
}

// delay returns how long this attempt should wait before it is answered.
func (t *loginThrottle) delay() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failed < kLoginFreeTries {
		return 0
	}
	d := time.Second << min(t.failed-kLoginFreeTries, 3)
	return min(d, kMaxLoginDelay)
}

func (t *loginThrottle) failure() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed++
}

func (t *loginThrottle) success() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed = 0
}

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

// Prod sits behind nginx, so r.TLS is nil: Secure follows X-Forwarded-Proto.
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
	httpjson.WriteOK(w, setupStatusResponse{Needed: err != nil})
}

func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db.GetSettings(); err == nil {
		http.NotFound(w, r)
		return
	}
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Email == "" || len(req.Password) < 8 {
		httpjson.WriteBadRequest(w, "email and password (min 8 chars) required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: req.Email, PasswordHash: string(hash)}); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if err := h.setSession(w, r); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, okStatusResponse{Status: "created"})
}

// The owner sets their own password over a one-time link; the token burns on use.
func (h *Handler) Invite(w http.ResponseWriter, r *http.Request) {
	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Password) < 8 {
		httpjson.WriteBadRequest(w, "token and password (min 8 chars) required")
		return
	}
	if !h.db.ValidToken(req.Token, kPurposeInvite) {
		httpjson.WriteUnauthorized(w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if err := h.db.SetOwnerPassword(string(hash)); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if err := h.db.UseToken(req.Token); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if err := h.setSession(w, r); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, okStatusResponse{Status: "ok"})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	// Paid before the answer, or its length leaks whether the guess was right.
	if d := h.login.delay(); d > 0 {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
	}
	s, err := h.db.GetSettings()
	if err != nil || s.OwnerEmail != req.Email ||
		bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(req.Password)) != nil {
		h.login.failure()
		httpjson.WriteUnauthorized(w)
		return
	}
	h.login.success()
	if err := h.setSession(w, r); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, okStatusResponse{Status: "ok"})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	if len(req.NewPassword) < 8 {
		httpjson.WriteBadRequest(w, "new password must be at least 8 characters")
		return
	}
	s, err := h.db.GetSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(req.CurrentPassword)) != nil {
		httpjson.WriteUnauthorized(w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	s.PasswordHash = string(hash)
	if err := h.db.UpdateSettings(s); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	// The cookie has already been checked by SessionAuth, so it is definitely there.
	if c, err := r.Cookie("session"); err == nil {
		if err := h.db.DeleteOtherTokens(c.Value); err != nil {
			httpjson.WriteInternalError(w, err)
			return
		}
	}
	httpjson.WriteOK(w, okStatusResponse{Status: "ok"})
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
	httpjson.WriteOK(w, okStatusResponse{Status: "ok"})
}

func (h *Handler) SessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil || !h.db.ValidToken(c.Value, "session") {
			httpjson.WriteUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
