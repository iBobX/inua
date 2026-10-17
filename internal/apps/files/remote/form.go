package remote

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Kinds are the kinds a disk is set up as, in the setup's order.
var Kinds = []Kind{SFTP, SMB, WebDAV, FTP, S3, R2}

// Form is a remote disk's fields as the setup asks for them, for its
// kind: a server and a folder, a share, a WebDAV URL, a bucket.
type Form struct {
	Kind   Kind
	Server string // the host; S3's endpoint; R2's account ID
	Port   string
	User   string // the user; the access key's ID for S3 and R2
	Share  string // SMB
	Bucket string // S3, R2
	Folder string // below the share or the bucket, or on the server
	URL    string // WebDAV: the URL its server's settings show
	TLS    bool   // FTP over TLS (FTPS)
}

// What the setup's fields can lack, past what Parse says.
var (
	ErrNoBucket = errors.New("a bucket is needed")
	ErrNoURL    = errors.New("a WebDAV URL starts with https:// or http://")
	ErrPort     = errors.New("a port is a number from 1 to 65535")
)

// Address is the form's address.
func (f Form) Address() (Address, error) {
	trim := func(s string) string { return strings.Trim(strings.TrimSpace(s), "/") }
	a := Address{Kind: f.Kind, Scheme: string(f.Kind), User: strings.TrimSpace(f.User), Port: strings.TrimSpace(f.Port)}
	host := trim(f.Server)
	// An endpoint pasted as a URL is its host.
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Host
	}
	if h, p, ok := strings.Cut(host, ":"); ok && a.Port == "" && strings.Count(host, ":") == 1 {
		host, a.Port = h, p // host:port; an IPv6 address has more colons
	}
	a.Host = host
	switch f.Kind {
	case SMB:
		a.Path = join(trim(f.Share), trim(f.Folder))
		if trim(f.Share) == "" {
			return Address{}, ErrNoShare
		}
	case S3, R2:
		if trim(f.Bucket) == "" {
			return Address{}, ErrNoBucket
		}
		a.Path = join(trim(f.Bucket), trim(f.Folder))
	case WebDAV:
		u, err := url.Parse(strings.TrimSpace(f.URL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return Address{}, ErrNoURL
		}
		a.Scheme = "davs"
		if u.Scheme == "http" {
			a.Scheme = "dav"
		}
		a.Host, a.Port, a.Path = u.Hostname(), u.Port(), trim(u.Path)
		if u.User != nil && a.User == "" {
			a.User = u.User.Username()
		}
	case FTP:
		if f.TLS {
			a.Scheme = "ftps"
		}
		a.Path = trim(f.Folder)
	default:
		a.Path = trim(f.Folder)
	}
	if a.Port != "" {
		if n, err := strconv.Atoi(a.Port); err != nil || n < 1 || n > 65535 {
			return Address{}, ErrPort
		}
	}
	// Parse checks what is left (a server, line breaks) as for a typed address.
	return Parse(a.String())
}

func join(a, b string) string {
	if b == "" {
		return a
	}
	if a == "" {
		return b
	}
	return a + "/" + b
}

// FormOf is an address's fields, for its setup to be changed.
func FormOf(a Address) Form {
	f := Form{Kind: a.Kind, Server: a.Host, Port: a.Port, User: a.User, Folder: a.Path}
	switch a.Kind {
	case SMB:
		f.Share, f.Folder, _ = strings.Cut(a.Path, "/")
	case S3, R2:
		f.Bucket, f.Folder, _ = strings.Cut(a.Path, "/")
	case WebDAV:
		scheme := "https"
		if a.Scheme == "dav" {
			scheme = "http"
		}
		host := a.Host
		if a.Port != "" {
			host += ":" + a.Port
		}
		f.URL = (&url.URL{Scheme: scheme, Host: host, Path: "/" + a.Path}).String()
		f.Server, f.Port, f.Folder = "", "", ""
	case FTP:
		f.TLS = a.Scheme == "ftps"
	}
	return f
}
