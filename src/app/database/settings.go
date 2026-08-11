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

// Алфавит без визуально спутываемых символов (0/O, 1/l/I) — пароль
// диктуют по SSH или читают с экрана.
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
		 ga_measurement_id, metrika_counter_id
		 FROM settings WHERE id=1`).Scan(
		&s.OwnerEmail, &s.PasswordHash, &s.ShopName, &s.ShopPhone, &s.SMTPHost,
		&s.SMTPPort, &s.SMTPUser, &s.SMTPPassword, &s.Currency, &s.Lang, &s.Logo,
		&s.GAMeasurementID, &s.MetrikaCounterID)
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
		 lang=?, logo=?, ga_measurement_id=?, metrika_counter_id=?
		 WHERE id=1`,
		s.OwnerEmail, s.PasswordHash, s.ShopName, s.ShopPhone, s.SMTPHost,
		s.SMTPPort, s.SMTPUser, s.SMTPPassword, currency, lang, s.Logo,
		s.GAMeasurementID, s.MetrikaCounterID)
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

// CleanupExpiredTokens удаляет протухшие токены; вызывается один раз при
// старте — таблица не растёт бесконечно на долгоживущем инстансе.
func (d *Database) CleanupExpiredTokens() error {
	_, err := d.db.Exec(`DELETE FROM auth_tokens WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}

// DeleteOtherTokens инвалидирует все сессии, кроме той, из которой вызвана
// смена пароля: угнанная кука умирает вместе со старым паролем, а вкладку,
// где пароль сменили, из-под владельца не выбивает.
func (d *Database) DeleteOtherTokens(except string) error {
	_, err := d.db.Exec(`DELETE FROM auth_tokens WHERE token != ?`, except)
	return err
}

// CreateOwner закрывает окно, в котором свежий инстанс отдаёт открытый
// мастер настройки: владельца заводит провижининг, а не первый, кто угадал
// адрес. Возвращает пароль в открытом виде один раз.
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

// ResetOwnerPassword — путь восстановления через CLI (`fastoshop
// -reset-password`): генерирует новый пароль, сохраняет его bcrypt-хэш и
// сносит все сессии одним махом, чтобы угнанная кука не пережила смену.
// Возвращает пароль в открытом виде один раз — нигде не хранится и не
// логируется.
// NewInviteToken — одноразовая ссылка на задание пароля. Живёт сутки: её
// пересылают письмом, и просроченная ссылка безопаснее забытой действующей.
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

// SetOwnerPassword ставит пароль, выбранный самим владельцем по одноразовой
// ссылке. Сессии не трогаем: приглашение открывают до первого входа, а рвать
// чужие сессии по чужой ссылке — готовый способ выкинуть владельца из админки.
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
