package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Where the address came from: the footer form or the tick in an order. The
// seller is asked to prove consent, and "they typed it somewhere" is not proof.
const (
	SubscribeFooter = "footer"
	SubscribeOrder  = "order"
)

type Subscriber struct {
	ID             int64
	Email          string
	Source         string
	ConsentText    string
	Token          string
	CreatedAt      time.Time
	UnsubscribedAt sql.NullTime
}

// Subscribe stores the address once. A second attempt is not an error and does
// not reset the token: the buyer keeps the unsubscribe link from the first letter.
func (d *Database) Subscribe(email, source, consent string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("subscribe: %q is not an address", email)
	}
	token, err := newToken()
	if err != nil {
		return fmt.Errorf("subscribe token: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT INTO subscribers (email, source, consent_text, token) VALUES (?, ?, ?, ?)
		 ON CONFLICT(email) DO UPDATE SET unsubscribed_at = NULL`,
		email, source, consent, token)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return nil
}

// Unsubscribe marks the address by its token; an unknown token is not an error,
// or the link would tell a stranger which addresses exist.
func (d *Database) Unsubscribe(token string) error {
	_, err := d.db.Exec(
		`UPDATE subscribers SET unsubscribed_at = CURRENT_TIMESTAMP
		 WHERE token = ? AND unsubscribed_at IS NULL`, token)
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	return nil
}

// Subscribers lists the addresses, newest first, for the owner's export.
func (d *Database) Subscribers() ([]Subscriber, error) {
	rows, err := d.db.Query(
		`SELECT id, email, source, consent_text, token, created_at, unsubscribed_at
		 FROM subscribers ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("subscribers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Subscriber{}
	for rows.Next() {
		var s Subscriber
		if err := rows.Scan(&s.ID, &s.Email, &s.Source, &s.ConsentText, &s.Token,
			&s.CreatedAt, &s.UnsubscribedAt); err != nil {
			return nil, fmt.Errorf("subscribers: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SubscriberCount is what the admin shows next to the export.
func (d *Database) SubscriberCount() (active, total int, err error) {
	err = d.db.QueryRow(
		`SELECT COUNT(*) FILTER (WHERE unsubscribed_at IS NULL), COUNT(*) FROM subscribers`).
		Scan(&active, &total)
	if err != nil {
		return 0, 0, fmt.Errorf("subscriber count: %w", err)
	}
	return active, total, nil
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
