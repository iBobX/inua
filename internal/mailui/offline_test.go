package mailui

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
)

// An account that cannot connect says why in a few words, whatever the
// error is wrapped in.
func TestOffline(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("imap: %w", err) }
	for _, c := range []struct {
		err  error
		want string
	}{
		{wrap(&net.DNSError{Err: "no such host", Name: "imap.example.com", IsNotFound: true}), "Offline: server not found"},
		{wrap(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}), "Offline: connection refused"},
		{wrap(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ENETUNREACH)}), "Offline: no network"},
		{wrap(&tls.CertificateVerificationError{Err: errors.New("x509: certificate signed by unknown authority")}), "Offline: certificate not trusted"},
		{wrap(context.DeadlineExceeded), "Offline: no answer from the server"},
		{wrap(&net.OpError{Op: "read", Net: "tcp", Err: timeout{}}), "Offline: no answer from the server"},
		{wrap(io.EOF), "Offline: connection dropped"},
		{wrap(os.NewSyscallError("read", syscall.ECONNRESET)), "Offline: connection dropped"},
		{errors.New("imap: BAD something else"), "Offline"},
	} {
		if got := offline(c.err); got != c.want {
			t.Errorf("offline(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

type timeout struct{}

func (timeout) Error() string   { return "i/o timeout" }
func (timeout) Timeout() bool   { return true }
func (timeout) Temporary() bool { return true }
