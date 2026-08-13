package mail

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net/smtp"

	"github.com/fastogt/fastoshop/app/database"
)

func buildMessage(from, to, subject, body string) []byte {
	encSubject := mime.BEncoding.Encode("UTF-8", subject)
	return []byte("From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + encSubject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body + "\r\n")
}

// Send emails the owner via the SMTP from settings. Port 465 = implicit TLS
// (Yandex 360 / VK WorkSpace), hence tls.Dial rather than smtp.SendMail (the
// latter only speaks STARTTLS).
func Send(s *database.Settings, subject, body string) error {
	if s.SMTPHost == "" {
		return fmt.Errorf("smtp not configured")
	}
	addr := fmt.Sprintf("%s:%d", s.SMTPHost, s.SMTPPort)
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.SMTPHost})
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	c, err := smtp.NewClient(conn, s.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Auth(smtp.PlainAuth("", s.SMTPUser, s.SMTPPassword, s.SMTPHost)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := c.Mail(s.SMTPUser); err != nil {
		return err
	}
	if err := c.Rcpt(s.OwnerEmail); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(buildMessage(s.SMTPUser, s.OwnerEmail, subject, body)); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}
