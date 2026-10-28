package webui

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
)

var errAccountMail = errors.New("Bestätigungs-E-Mail konnte nicht versendet werden; bitte den Administrator kontaktieren")

func validMailConfig(cfg types.MailConfig) bool {
	if cfg.Transport == "jmap" {
		return validJMAPConfig(cfg)
	}

	return (cfg.Transport == "" || cfg.Transport == "smtp") && validSMTPConfig(cfg)
}

func validSMTPConfig(cfg types.MailConfig) bool {
	from, err := db.NormalizeAccountEmail(cfg.From)

	return err == nil && from != "" && cfg.Host != "" && !strings.ContainsAny(cfg.Host, "\r\n /\\") &&
		cfg.Port > 0 && cfg.Port <= 65535 && (cfg.TLSMode == "starttls" || cfg.TLSMode == "tls") &&
		!strings.ContainsAny(cfg.Username, "\r\n") && (cfg.Password == "" || cfg.PasswordPath == "") && (cfg.Username == "" || cfg.Password != "" || cfg.PasswordPath != "")
}

func sendAccountMail(ctx context.Context, cfg types.MailConfig, recipient, body string) error {
	if cfg.Transport == "jmap" {
		return deliverJMAPMail(ctx, cfg, recipient, body, nil)
	}

	if cfg.Transport != "" && cfg.Transport != "smtp" {
		return errAccountMail
	}

	return deliverAccountMail(ctx, cfg, recipient, body, nil)
}

// Root overrides are private to local TLS tests; production always uses system trust.
func deliverAccountMail(ctx context.Context, cfg types.MailConfig, recipient, body string, roots *x509.CertPool) error {
	if !validSMTPConfig(cfg) {
		return errAccountMail
	}

	if strings.ContainsAny(recipient, "\r\n") {
		return errAccountMail
	}

	to, err := db.NormalizeAccountEmail(recipient)
	if err != nil || strings.ContainsAny(body, "\x00") {
		return errAccountMail
	}

	from, err := db.NormalizeAccountEmail(cfg.From)
	if err != nil {
		return errAccountMail
	}

	secret := cfg.Password
	if cfg.PasswordPath != "" {
		b, err := os.ReadFile(cfg.PasswordPath)
		if err != nil || len(b) > 4096 {
			return errAccountMail
		}

		secret = strings.TrimSpace(string(b))
		clear(b)
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	dialer := net.Dialer{}

	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return errAccountMail
	}
	defer connection.Close()

	deadline, _ := ctx.Deadline()

	err = connection.SetDeadline(deadline)
	if err != nil {
		return errAccountMail
	}

	rawConnection := connection

	stop := context.AfterFunc(ctx, func() { _ = rawConnection.Close() })
	defer stop()

	tlsConfig := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, RootCAs: roots}
	if cfg.TLSMode == "tls" {
		secure := tls.Client(connection, tlsConfig)

		err := secure.HandshakeContext(ctx)
		if err != nil {
			return errAccountMail
		}

		connection = secure
	}

	client, err := smtp.NewClient(connection, cfg.Host)
	if err != nil {
		return errAccountMail
	}
	defer client.Close()

	if cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errAccountMail
		}

		err := client.StartTLS(tlsConfig)
		if err != nil {
			return errAccountMail
		}
	}

	if cfg.Username != "" {
		err := client.Auth(smtp.PlainAuth("", cfg.Username, secret, cfg.Host))
		if err != nil {
			return errAccountMail
		}
	}

	err = client.Mail(from)
	if err != nil {
		return errAccountMail
	}

	//nolint:gosec // G707: NormalizeAccountEmail uses mail.ParseAddress; CR and LF are explicitly rejected before SMTP.
	err = client.Rcpt(to)
	if err != nil {
		return errAccountMail
	}

	writer, err := client.Data()
	if err != nil {
		return errAccountMail
	}

	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nDate: %s\r\nSubject: Meshora: E-Mail-Adresse bestätigen\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", from, to, time.Now().UTC().Format(time.RFC1123Z), strings.ReplaceAll(body, "\n", "\r\n"))

	_, err = writer.Write([]byte(message))
	if err != nil {
		return errAccountMail
	}

	err = writer.Close()
	if err != nil {
		return errAccountMail
	}

	err = client.Quit()
	if err != nil {
		return errAccountMail
	}

	return nil
}
