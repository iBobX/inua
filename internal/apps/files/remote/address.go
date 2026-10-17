// Package remote mounts remote disks with rclone: a server's folder or a
// bucket, given by an address, mounted at ~/Remote/NAME so that every
// program reads and writes it as a local folder. It does not import GTK.
package remote

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Kind is what a remote disk is on, which is rclone's backend for it.
type Kind string

const (
	SFTP   Kind = "sftp"
	SMB    Kind = "smb"
	FTP    Kind = "ftp"
	WebDAV Kind = "webdav"
	S3     Kind = "s3"
	R2     Kind = "r2"
)

// schemes are the addresses' schemes, and what each is on.
var schemes = map[string]Kind{
	"sftp": SFTP, "smb": SMB, "ftp": FTP, "ftps": FTP,
	"dav": WebDAV, "davs": WebDAV, "s3": S3, "r2": R2,
}

// Address is a remote disk's address, read: what is on the other side,
// without its secret.
type Address struct {
	Scheme string // as written: sftp, smb, ftp, ftps, dav, davs, s3, r2
	Kind   Kind
	User   string // the user, or the access key's ID for S3 and R2
	Host   string // the server; R2's account ID
	Port   string
	Path   string // below the host, without the slashes around it: the share, the bucket, a folder
}

// SchemeError is an address whose scheme no remote disk is on.
type SchemeError struct{ Scheme string }

func (e *SchemeError) Error() string { return fmt.Sprintf("no remote disk is on %s://", e.Scheme) }

// What can be wrong with an address. The window words them.
var (
	// ErrPassword is an address with a password in it: the password is
	// asked for, and kept in the keyring, not in the address that is saved.
	ErrPassword  = errors.New("the password goes in its own field, not in the address")
	ErrNoScheme  = errors.New("an address starts with sftp://, smb://, s3:// …")
	ErrNoServer  = errors.New("the address names no server")
	ErrNoShare   = errors.New("an SMB address names a share: smb://server/share")
	ErrLineBreak = errors.New("the address has a line break in it")
)

// Parse reads an address: sftp://user@host:22/folder, smb://server/share,
// davs://host/folder, ftp(s)://host, s3://KEY@host/bucket (s3.amazonaws.com
// or another S3 server) and r2://KEY@ACCOUNT/bucket.
func Parse(s string) (Address, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return Address{}, ErrNoScheme
	}
	scheme := strings.ToLower(u.Scheme)
	kind, ok := schemes[scheme]
	if !ok {
		if scheme == "" {
			return Address{}, ErrNoScheme
		}
		return Address{}, &SchemeError{Scheme: scheme}
	}
	if _, set := u.User.Password(); set {
		return Address{}, ErrPassword
	}
	a := Address{Scheme: scheme, Kind: kind, User: u.User.Username(), Host: u.Hostname(), Port: u.Port(), Path: strings.Trim(u.Path, "/")}
	if a.Host == "" {
		return Address{}, ErrNoServer
	}
	if a.Kind == SMB && a.Path == "" {
		return Address{}, ErrNoShare
	}
	for _, v := range []string{a.User, a.Host, a.Port, a.Path} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return Address{}, ErrLineBreak
		}
	}
	return a, nil
}

// String is the address written out, as it is saved.
func (a Address) String() string {
	u := url.URL{Scheme: a.Scheme, Host: a.Host, Path: "/" + a.Path}
	if a.Port != "" {
		u.Host += ":" + a.Port
	}
	if a.User != "" {
		u.User = url.User(a.User)
	}
	if a.Path == "" {
		u.Path = ""
	}
	return u.String()
}

// WithUser is the address with another user, or access key ID.
func (a Address) WithUser(user string) Address {
	a.User = user
	return a
}

// Name is what a disk at the address is called at first: the share or the
// bucket, else the server.
func (a Address) Name() string {
	name := a.Host
	if first, _, _ := strings.Cut(a.Path, "/"); first != "" && (a.Kind == SMB || a.Kind == S3 || a.Kind == R2) {
		name = first
	}
	name = strings.TrimLeft(strings.Map(func(r rune) rune {
		if r == '/' || r < ' ' {
			return '-'
		}
		return r
	}, name), ".")
	if name == "" {
		name = string(a.Kind)
	}
	return name
}

// NeedsSecret says whether connecting needs a secret before it is tried:
// S3's and R2's keys always; a user's password on SMB, FTP and WebDAV
// (without a user they are guest or anonymous). SFTP tries the SSH agent
// first and asks only when the server refuses it.
func (a Address) NeedsSecret() bool {
	switch a.Kind {
	case S3, R2:
		return true
	case SMB, FTP, WebDAV:
		return a.User != ""
	}
	return false
}

// KeyPair says whether the address signs in with an access key, an ID
// and a secret, rather than a user and a password.
func (a Address) KeyPair() bool { return a.Kind == S3 || a.Kind == R2 }
