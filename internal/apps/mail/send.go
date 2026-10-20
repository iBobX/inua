package mail

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/emersion/go-smtp"
)

// DialSMTP connects to an SMTP server: TLS on port 465, else STARTTLS.
// A test replaces it to reach a server without TLS.
var DialSMTP = func(addr string) (*smtp.Client, error) {
	_, port, _ := net.SplitHostPort(addr)
	if port == "465" {
		return smtp.DialTLS(addr, nil)
	}
	return smtp.DialStartTLS(addr, nil)
}

// Send sends a built message over SMTP, signed in with auth.
func Send(ctx context.Context, addr string, auth Auth, b Built) error {
	if len(b.Recipients) == 0 {
		return ErrNoRecipient
	}
	done := make(chan error, 1)
	var c *smtp.Client
	go func() {
		var err error
		c, err = DialSMTP(addr)
		if err != nil {
			done <- err
			return
		}
		done <- send(ctx, c, auth, b)
	}()
	select {
	case err := <-done:
		if c != nil {
			c.Close()
		}
		return err
	case <-ctx.Done():
		go func() {
			<-done
			if c != nil {
				c.Close()
			}
		}()
		return ctx.Err()
	}
}

func send(ctx context.Context, c *smtp.Client, auth Auth, b Built) error {
	for try := 0; ; try++ {
		cl, err := auth.sasl(ctx)
		if err != nil {
			return err
		}
		err = c.Auth(cl)
		if err == nil {
			break
		}
		if auth.Tokens == nil || try > 0 {
			return fmt.Errorf("%w: %v", ErrAuth, err)
		}
		auth.Tokens.Expire()
	}
	if err := c.SendMail(b.From, b.Recipients, bytes.NewReader(b.Raw)); err != nil {
		return err
	}
	return c.Quit()
}

// httpDetect sniffs a file's type from its first bytes.
func httpDetect(data []byte) string { return http.DetectContentType(data) }
