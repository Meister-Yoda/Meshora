package webui

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

var (
	errTestSMTPPlaintext = errors.New("SMTP sent before TLS")
	errTestSMTPCommand   = errors.New("unexpected SMTP command")
)

func TestAccountMailUsesVerifiedTLSAndSendsConfirmation(t *testing.T) {
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			fixture := httptest.NewTLSServer(nil)
			certificate := fixture.TLS.Certificates[0]
			roots := x509.NewCertPool()
			roots.AddCert(fixture.Certificate())
			fixture.Close()

			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			require.NoError(t, err)

			defer listener.Close()

			finished := make(chan error, 1)
			messages := make(chan string, 1)

			go func() {
				connection, err := listener.Accept()
				if err != nil {
					finished <- err
					return
				}
				defer connection.Close()

				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				finished <- testSMTPConversation(connection, certificate, mode, messages)
			}()

			address, ok := listener.Addr().(*net.TCPAddr)
			require.True(t, ok)
			cfg := types.MailConfig{Host: "127.0.0.1", Port: address.Port, From: "meshora@example.test", TLSMode: mode, Username: "mailer", Password: t.Name()}
			body := "Confirm your request:\nhttps://meshora.example/account/email/confirm#token=example-token"
			require.NoError(t, deliverAccountMail(t.Context(), cfg, "user@example.test", body, roots))
			require.NoError(t, <-finished)

			message := <-messages
			require.Contains(t, message, "To: user@example.test")
			require.Contains(t, message, "#token=example-token")
			require.NotContains(t, message, cfg.Password)
		})
	}
}

func testSMTPConversation(raw net.Conn, certificate tls.Certificate, mode string, messages chan<- string) error {
	connection := raw
	secured := mode == "tls"

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	if secured {
		connection = tls.Server(connection, tlsConfig)
	}

	write := func(line string) error { _, err := io.WriteString(connection, line+"\r\n"); return err }

	err := write("220 localhost SMTP ready")
	if err != nil {
		return err
	}

	reader := bufio.NewReader(connection)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "EHLO"):
			if secured {
				err = write("250-localhost\r\n250 AUTH PLAIN")
			} else {
				err = write("250-localhost\r\n250 STARTTLS")
			}
		case line == "STARTTLS":
			err = write("220 ready")
			if err != nil {
				return err
			}

			secure := tls.Server(connection, tlsConfig)

			err = secure.HandshakeContext(context.Background())
			if err != nil {
				return err
			}

			connection = secure
			reader = bufio.NewReader(connection)
			secured = true
		case strings.HasPrefix(line, "AUTH "):
			if !secured {
				return errTestSMTPPlaintext
			}

			err = write("235 authenticated")
		case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
			if !secured {
				return errTestSMTPPlaintext
			}

			err = write("250 accepted")
		case line == "DATA":
			err = write("354 send data")
			if err != nil {
				return err
			}

			body, readErr := textproto.NewReader(reader).ReadDotBytes()
			if readErr != nil {
				return readErr
			}

			messages <- string(body)

			err = write("250 delivered")
		case line == "QUIT":
			return write("221 closing")
		default:
			return errTestSMTPCommand
		}

		if err != nil {
			return err
		}
	}
}

func TestAccountMailRejectsUntrustedTLSAndPlaintext(t *testing.T) {
	fixture := httptest.NewTLSServer(nil)
	certificate := fixture.TLS.Certificates[0]
	fixture.Close()

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	require.NoError(t, err)

	defer listener.Close()

	finished := make(chan struct{})
	go func() {
		defer close(finished)

		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()

		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.WriteString(connection, "220 SMTP ready\r\n")
	}()

	address, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	cfg := types.MailConfig{Host: "127.0.0.1", Port: address.Port, From: "meshora@example.test", TLSMode: "tls"}
	require.ErrorIs(t, sendAccountMail(t.Context(), cfg, "user@example.test", "body"), errAccountMail)
	<-finished

	cfg.TLSMode = "plain"
	require.ErrorIs(t, sendAccountMail(t.Context(), cfg, "user@example.test", "body"), errAccountMail)
	cfg.TLSMode = "starttls"
	require.ErrorIs(t, sendAccountMail(t.Context(), cfg, "bad\r\nBcc: secret@example.test", "body"), errAccountMail)
}

func TestAccountMailCancellationClosesTransport(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	finished := make(chan struct{})
	go func() {
		defer close(finished)

		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()

		_, _ = io.Copy(io.Discard, connection)
	}()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	number, err := strconv.Atoi(port)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, sendAccountMail(ctx, types.MailConfig{Host: host, Port: number, From: "meshora@example.test", TLSMode: "starttls"}, "user@example.test", "body"), errAccountMail)

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancelled SMTP socket was not closed")
	}
}
