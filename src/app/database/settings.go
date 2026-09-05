package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/fastogt/fastoshop/app/i18n"
)

// Alphabet without visually confusable characters (0/O, 1/l/I): it gets dictated.
const kGeneratedPasswordAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"
const kGeneratedPasswordLength = 16

const (
	ShopCurrencyRUB = "RUB"
	ShopCurrencyBYN = "BYN"
	ShopCurrencyPLN = "PLN"
	ShopCurrencyKZT = "KZT"
)

// kCurrencySigns is what a buyer sees next to the number; every sign goes after it.
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
	// Messenger handles stored as the owner typed them; normalised where the link is built.
	Telegram string `json:"telegram"`
	WhatsApp string `json:"whatsapp"`
	// Legal footer details, free-form: their shape differs by country and seller type.
	Requisites string `json:"requisites"`
	// Delivery, payment and returns, free-form, at /info; search engines check for it.
	Terms string `json:"terms"`
	// Logo file name; empty means the shop is represented by its name.
	Logo string `json:"logo"`
	// Shop-wide currency: one shop sells in one country's money.
	Currency string `json:"currency"`
	// Owner's language; drives the admin and the text the server renders for them.
	Lang     string `json:"lang"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPUser string `json:"smtp_user"`
	// Envelope and header sender; empty means the SMTP login is used.
	SMTPFrom     string `json:"smtp_from"`
	SMTPPassword string `json:"-"`
	// Stored raw: a format check would break the day a provider changes its own.
	GAMeasurementID  string `json:"ga_measurement_id"`
	MetrikaCounterID string `json:"metrika_counter_id"`
	// A secret like the SMTP password: it never leaves the server.
	AdHuntersAPIKey string `json:"-"`
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
		 ga_measurement_id, metrika_counter_id, requisites, smtp_from, terms,
		 adhunters_api_key, telegram, whatsapp
		 FROM settings WHERE id=1`).Scan(
		&s.OwnerEmail, &s.PasswordHash, &s.ShopName, &s.ShopPhone, &s.SMTPHost,
		&s.SMTPPort, &s.SMTPUser, &s.SMTPPassword, &s.Currency, &s.Lang, &s.Logo,
		&s.GAMeasurementID, &s.MetrikaCounterID, &s.Requisites, &s.SMTPFrom, &s.Terms,
		&s.AdHuntersAPIKey, &s.Telegram, &s.WhatsApp)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *Database) UpdateSettings(s *Settings) error {
	currency := s.Currency
	// Empty means "not set yet": default rather than refuse to save.
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
		 lang=?, logo=?, ga_measurement_id=?, metrika_counter_id=?, requisites=?,
		 smtp_from=?, terms=?, adhunters_api_key=?, telegram=?, whatsapp=?
		 WHERE id=1`,
		s.OwnerEmail, s.PasswordHash, s.ShopName, s.ShopPhone, s.SMTPHost,
		s.SMTPPort, s.SMTPUser, s.SMTPPassword, currency, lang, s.Logo,
		s.GAMeasurementID, s.MetrikaCounterID, s.Requisites, s.SMTPFrom, s.Terms,
		s.AdHuntersAPIKey, strings.TrimSpace(s.Telegram), strings.TrimSpace(s.WhatsApp))
	return err
}

// A broken settings row must not blank out the message the owner needs to read.
func (d *Database) Lang() string {
	s, err := d.GetSettings()
	if err != nil {
		return i18n.LangRU
	}
	return s.Lang
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
		 WHERE token=? AND purpose=? AND expires_at > CURRENT_TIMESTAMP`,
		token, purpose).Scan(&n)
	return err == nil && n > 0
}

func (d *Database) UseToken(token string) error {
	_, err := d.db.Exec(`DELETE FROM auth_tokens WHERE token=?`, token)
	return err
}

func (d *Database) CleanupExpiredTokens() error {
	_, err := d.db.Exec(`DELETE FROM auth_tokens WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}

// A hijacked cookie dies with the old password; the tab that changed it survives.
func (d *Database) DeleteOtherTokens(except string) error {
	_, err := d.db.Exec(`DELETE FROM auth_tokens WHERE token != ?`, except)
	return err
}

// The owner is created by provisioning, not by whoever guessed the address first.
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

// A one-time link for setting the password; it is forwarded by email.
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

// Sessions are left alone: someone else's invite must not throw the owner out.
func (d *Database) SetOwnerPassword(hash string) error {
	_, err := d.db.Exec(`UPDATE settings SET password_hash=? WHERE id=1`, hash)
	return err
}

// The plaintext password is returned once: it is stored nowhere and never logged.
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

// Sign lives next to the currency code so the storefront never maps it itself.
func (s *Settings) Sign() string {
	if sign, ok := kCurrencySigns[s.Currency]; ok {
		return sign
	}
	return kCurrencySigns[ShopCurrencyRUB]
}
