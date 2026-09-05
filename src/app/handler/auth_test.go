package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/fastogt/fastoshop/app/database"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewHandler(d, t.TempDir())
}

func TestSetupLoginFlow(t *testing.T) {
	h := newTestHandler(t)

	// Setup is available while there is no owner yet.
	w := httptest.NewRecorder()
	h.SetupStatus(w, httptest.NewRequest("GET", "/api/setup", nil))
	if !strings.Contains(w.Body.String(), `"needed":true`) {
		t.Fatalf("setup status: %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	h.Setup(w, httptest.NewRequest("POST", "/api/setup",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()
	if len(cookie) == 0 || cookie[0].Name != "session" {
		t.Fatal("setup must set session cookie")
	}

	// A second setup - 404.
	w = httptest.NewRecorder()
	h.Setup(w, httptest.NewRequest("POST", "/api/setup",
		strings.NewReader(`{"email":"x@y.z","password":"hack"}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("second setup must 404, got %d", w.Code)
	}

	// Wrong password - 401.
	w = httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("POST", "/api/login",
		strings.NewReader(`{"email":"a@b.c","password":"wrong"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d", w.Code)
	}

	// Correct one - a cookie.
	w = httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("POST", "/api/login",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	if w.Code != http.StatusOK || len(w.Result().Cookies()) == 0 {
		t.Fatalf("login: %d", w.Code)
	}
	sess := w.Result().Cookies()[0]

	// Middleware: without the cookie - 401, with it - 200.
	protected := h.SessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, httptest.NewRequest("GET", "/api/products", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", w.Code)
	}
	req := httptest.NewRequest("GET", "/api/products", nil)
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("with cookie: %d", w.Code)
	}
}

// The session cookie carries Secure only where the connection is TLS.
func TestInvite(t *testing.T) {
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	h := NewHandler(d, t.TempDir())
	if _, err := d.CreateOwner("owner@example.com"); err != nil {
		t.Fatal(err)
	}
	tok, err := d.NewInviteToken()
	if err != nil {
		t.Fatal(err)
	}

	body := func(tok, pw string) string {
		return fmt.Sprintf(`{"token":%q,"password":%q}`, tok, pw)
	}
	post := func(b string) int {
		w := httptest.NewRecorder()
		h.Invite(w, httptest.NewRequest("POST", "/api/invite", strings.NewReader(b)))
		return w.Code
	}

	if code := post(body("deadbeef", "hunter2hunter")); code != http.StatusUnauthorized {
		t.Errorf("wrong token accepted: %d", code)
	}
	if code := post(body(tok, "short")); code != http.StatusBadRequest {
		t.Errorf("short password accepted: %d", code)
	}
	if code := post(body(tok, "hunter2hunter")); code != http.StatusOK {
		t.Fatalf("invite rejected: %d", code)
	}
	if code := post(body(tok, "another-password")); code != http.StatusUnauthorized {
		t.Errorf("token reusable: %d", code)
	}

	s, _ := d.GetSettings()
	if bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte("hunter2hunter")) != nil {
		t.Error("password not set by invite")
	}
}

func TestSessionCookieSecureFlag(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Setup(w, httptest.NewRequest("POST", "/api/setup",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	if c := w.Result().Cookies(); len(c) == 0 || c[0].Secure {
		t.Fatalf("plain http must not set Secure: %+v", c)
	}

	req := httptest.NewRequest("POST", "/api/login",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	h.Login(w, req)
	c := w.Result().Cookies()
	if len(c) == 0 || !c[0].Secure {
		t.Fatalf("https must set Secure: %+v", c)
	}
}

func TestChangePassword(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Setup(w, httptest.NewRequest("POST", "/api/setup",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	sess := w.Result().Cookies()[0]

	// Wrong current password - 401, the hash stays unchanged.
	req := httptest.NewRequest("POST", "/api/settings/password",
		strings.NewReader(`{"current_password":"wrong","new_password":"newpass123"}`))
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	h.ChangePassword(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("POST", "/api/login",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	if w.Code != http.StatusOK {
		t.Fatal("old password must still work after failed change")
	}

	// Too short a new password - 400.
	req = httptest.NewRequest("POST", "/api/settings/password",
		strings.NewReader(`{"current_password":"secret123","new_password":"short"}`))
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	h.ChangePassword(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short new password: %d %s", w.Code, w.Body.String())
	}

	// One more session - it must expire after the password change.
	w = httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("POST", "/api/login",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	otherSess := w.Result().Cookies()[0]

	// A valid password change.
	req = httptest.NewRequest("POST", "/api/settings/password",
		strings.NewReader(`{"current_password":"secret123","new_password":"newpass123"}`))
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	h.ChangePassword(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("change password: %d %s", w.Code, w.Body.String())
	}

	// The old password no longer works, the new one does.
	w = httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("POST", "/api/login",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatal("old password must no longer work")
	}
	w = httptest.NewRecorder()
	h.Login(w, httptest.NewRequest("POST", "/api/login",
		strings.NewReader(`{"email":"a@b.c","password":"newpass123"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("new password must work: %d %s", w.Code, w.Body.String())
	}

	protected := h.SessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// The session that changed the password stays valid.
	req = httptest.NewRequest("GET", "/api/products", nil)
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("calling session must stay valid: %d", w.Code)
	}

	// The other session is invalidated by the password change.
	req = httptest.NewRequest("GET", "/api/products", nil)
	req.AddCookie(otherSess)
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("other session must be invalidated: %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Setup(w, httptest.NewRequest("POST", "/api/setup",
		strings.NewReader(`{"email":"a@b.c","password":"secret123"}`)))
	sess := w.Result().Cookies()[0]

	req := httptest.NewRequest("POST", "/api/logout", nil)
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	h.Logout(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}

	protected := h.SessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req = httptest.NewRequest("GET", "/api/products", nil)
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out cookie must no longer authenticate: %d", w.Code)
	}
}

// The repository is public: internal error details (SQL, paths) never go out.
func TestInternalErrorDoesNotLeakDetails(t *testing.T) {
	h := newTestHandler(t)
	_ = h.db.Close()

	w := httptest.NewRecorder()
	h.ListProducts(w, httptest.NewRequest("GET", "/api/products", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sql") || strings.Contains(w.Body.String(), "closed") {
		t.Fatalf("internal details leaked: %s", w.Body.String())
	}
}

// Not a lockout: a shop has one owner and nobody to call, so a delay instead.
func TestLoginSlowsDownAfterRepeatedFailures(t *testing.T) {
	var th loginThrottle

	for i := 0; i < kLoginFreeTries; i++ {
		if d := th.delay(); d != 0 {
			t.Fatalf("try %d waited %v; the owner mistyping must not be punished", i+1, d)
		}
		th.failure()
	}
	first := th.delay()
	if first <= 0 {
		t.Fatalf("no delay after %d failures", kLoginFreeTries)
	}
	th.failure()
	if second := th.delay(); second <= first {
		t.Errorf("delay did not grow: %v then %v", first, second)
	}
	// Capped, so a forgotten password never becomes a locked shop.
	for range 20 {
		th.failure()
	}
	if d := th.delay(); d != kMaxLoginDelay {
		t.Errorf("delay %v, want the cap %v", d, kMaxLoginDelay)
	}
	// The right password clears the delay.
	th.success()
	if d := th.delay(); d != 0 {
		t.Errorf("after a success the delay is %v, want none", d)
	}
}
