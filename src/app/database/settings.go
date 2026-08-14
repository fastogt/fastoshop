package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/fastogt/fastoshop/app/i18n"
)

// Alphabet without visually confusable characters (0/O, 1/l/I) — the password
// gets dictated over SSH or read off a screen.
const kGeneratedPasswordAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"
const kGeneratedPasswordLength = 16

const (
	ShopCurrencyRUB = "RUB"
	ShopCurrencyBYN = "BYN"
	ShopCurrencyPLN = "PLN"
	ShopCurrencyKZT = "KZT"
)

// kCurrencySigns is what a buyer sees next to the number. Every sign here goes
// after the amount, which is how all four are written at home.
var kCurrencySigns = map[string]string{
	ShopCurrencyRUB: "₽",
	ShopCurrencyBYN: "Br",
	ShopCurrencyPLN: "zł",
	ShopCurrencyKZT: "₸",
}

func IsValidShopCurrency(c string) bool {
	_, ok := kCurrencySigns[c]
	return ok
}

type Settings struct {
	OwnerEmail   string `json:"owner_email"`
	PasswordHash string `json:"-"`
	ShopName     string `json:"shop_name"`
	ShopPhone    string `json:"shop_phone"`
	// Legal details shown in the storefront footer, free-form multiline: a shop
	// selling in Russia or Belarus is required to publish them, and their shape
	// differs by country and by whether the seller is a company or a sole
	// trader — a set of typed fields would fit one case and fight the rest.
	Requisites string `json:"requisites"`
	// Logo file name; empty means the shop is represented by its name.
	Logo string `json:"logo"`
	// Shop-wide currency: one shop sells in one country's money. Products carry
	// a currency column too, but it is the shop setting that reaches buyers and
	// search engines.
	Currency string `json:"currency"`
	// Owner's language. One shop has one owner, so this drives both the admin
	// and the text the server renders for them (errors, emails).
	Lang         string `json:"lang"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUser     string `json:"smtp_user"`
	SMTPPassword string `json:"-"`
	// Analytics counters and search-console ownership tokens, as issued by the
	// four cabinets. Stored raw: the shop only carries them to the page, and a
	// format check here would break the day a provider changes its own.
	GAMeasurementID  string `json:"ga_measurement_id"`
	MetrikaCounterID string `json:"metrika_counter_id"`
}

func (d *Database) CreateSettings(s *Settings) error {
	_, err := d.db.Exec(
		`INSERT INTO settings (id, owner_email, password_hash, shop_name, shop_phone)
		 VALUES (1, ?, ?, ?, ?)`,
		s.OwnerEmail, s.PasswordHash, s.ShopName, s.ShopPhone)
	return err
}

func (d *Database) GetSettings() (*Settings, error) {
	var s Settings
	err := d.db.QueryRow(
		`SELECT owner_email, password_hash, shop_name, shop_phone, smtp_host,
		 smtp_port, smtp_user, smtp_password, currency, lang, logo,
		 ga_measurement_id, metrika_counter_id, requisites
		 FROM settings WHERE id=1`).Scan(
		&s.OwnerEmail, &s.PasswordHash, &s.ShopName, &s.ShopPhone, &s.SMTPHost,
		&s.SMTPPort, &s.SMTPUser, &s.SMTPPassword, &s.Currency, &s.Lang, &s.Logo,
		&s.GAMeasurementID, &s.MetrikaCounterID, &s.Requisites)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *Database) UpdateSettings(s *Settings) error {
	currency := s.Currency
	// Empty means "not set yet" — a shop created before the setting existed, or
	// an older admin build. Default rather than refuse to save.
	if currency == "" {
		currency = ShopCurrencyRUB
	}
	if !IsValidShopCurrency(currency) {
		return fmt.Errorf("invalid shop currency: %q", currency)
	}
	lang := s.Lang
	if lang == "" {
		lang = i18n.LangRU
	}
	if !i18n.IsValidLang(lang) {
		return fmt.Errorf("invalid shop language: %q", lang)
	}
	_, err := d.db.Exec(
		`UPDATE settings SET owner_email=?, password_hash=?, shop_name=?, shop_phone=?,
		 smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, currency=?,
		 lang=?, logo=?, ga_measurement_id=?, metrika_counter_id=?, requisites=?
		 WHERE id=1`,
		s.OwnerEmail, s.PasswordHash, s.ShopName, s.ShopPhone, s.SMTPHost,
		s.SMTPPort, s.SMTPUser, s.SMTPPassword, currency, lang, s.Logo,
		s.GAMeasurementID, s.MetrikaCounterID, s.Requisites)
	return err
}

func (d *Database) CreateToken(token, purpose string, expires time.Time) error {
	_, err := d.db.Exec(
		`INSERT INTO auth_tokens (token, purpose, expires_at) VALUES (?, ?, ?)`,
		token, purpose, expires.UTC())
	return err
}

func (d *Database) ValidToken(token, purpose string) bool {
	var n int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM auth_tokens
		 WHERE token=? AND purpose=? AND used=0 AND expires_at > CURRENT_TIMESTAMP`,
		token, purpose).Scan(&n)
	return err == nil && n > 0
}

func (d *Database) UseToken(token string) error {
	_, err := d.db.Exec(`UPDATE auth_tokens SET used=1 WHERE token=?`, token)
	return err
}

// CleanupExpiredTokens deletes stale tokens; called once at startup — the
// table does not grow forever on a long-lived instance.
func (d *Database) CleanupExpiredTokens() error {
	_, err := d.db.Exec(`DELETE FROM auth_tokens WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}

// DeleteOtherTokens invalidates every session except the one the password
// change was made from: a hijacked cookie dies with the old password, while
// the tab where the password was changed does not kick the owner out.
func (d *Database) DeleteOtherTokens(except string) error {
	_, err := d.db.Exec(`DELETE FROM auth_tokens WHERE token != ?`, except)
	return err
}

// CreateOwner closes the window in which a fresh instance serves an open setup
// wizard: the owner is created by provisioning, not by whoever guessed the
// address first. Returns the plaintext password exactly once.
func (d *Database) CreateOwner(email string) (string, error) {
	if s, err := d.GetSettings(); err == nil {
		return "", fmt.Errorf("owner already exists (%s)", s.OwnerEmail)
	}
	pw, hash, err := generateCredentials()
	if err != nil {
		return "", err
	}
	if err := d.CreateSettings(&Settings{OwnerEmail: email, PasswordHash: hash}); err != nil {
		return "", err
	}
	return pw, nil
}

func generateCredentials() (string, string, error) {
	pw, err := generatePassword(kGeneratedPasswordLength)
	if err != nil {
		return "", "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	return pw, string(hash), nil
}

func generatePassword(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range raw {
		out[i] = kGeneratedPasswordAlphabet[int(b)%len(kGeneratedPasswordAlphabet)]
	}
	return string(out), nil
}

// ResetOwnerPassword — the recovery path via the CLI (`fastoshop
// -reset-password`): generates a new password, stores its bcrypt hash and
// wipes all sessions in one sweep so a hijacked cookie does not survive the
// change. Returns the plaintext password exactly once — it is stored nowhere
// and never logged.
// NewInviteToken — a one-time link for setting the password. Lives for a day:
// it is forwarded by email, and an expired link is safer than a forgotten
// working one.
func (d *Database) NewInviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	if err := d.CreateToken(tok, "invite", time.Now().Add(24*time.Hour)); err != nil {
		return "", err
	}
	return tok, nil
}

// SetOwnerPassword sets the password the owner chose themselves via the
// one-time link. Sessions are left alone: the invite is opened before the
// first login, and killing someone else's sessions via someone else's link is
// a ready-made way to throw the owner out of the admin.
func (d *Database) SetOwnerPassword(hash string) error {
	_, err := d.db.Exec(`UPDATE settings SET password_hash=? WHERE id=1`, hash)
	return err
}

func (d *Database) ResetOwnerPassword() (string, error) {
	pw, hash, err := generateCredentials()
	if err != nil {
		return "", err
	}
	err = d.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE settings SET password_hash=? WHERE id=1`, hash); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM auth_tokens`)
		return err
	})
	if err != nil {
		return "", err
	}
	return pw, nil
}

// Sign is kept next to the currency code so the storefront never has to map one
// to the other itself.
func (s *Settings) Sign() string {
	if sign, ok := kCurrencySigns[s.Currency]; ok {
		return sign
	}
	return kCurrencySigns[ShopCurrencyRUB]
}
