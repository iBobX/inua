package pass

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// Entry is a decrypted entry as pass writes one: the first line is the
// password; "key: value" lines are fields; an otpauth:// line is a
// one-time password; a line of dashes ("---") divides; what is left are
// notes.
type Entry struct {
	Password string
	Fields   []Field
	OTP      string // the otpauth:// URI, "" for none
	Notes    string
}

// Field is a "key: value" line.
type Field struct {
	Key, Value string
	// Secret is set when its name says it holds one (a PIN, a token …):
	// the app hides it as it hides the password.
	Secret bool
}

// fieldLine is a "key: value" line: a short key without spaces at its
// ends, a colon, a space.
var fieldLine = regexp.MustCompile(`^([^\s:][^:]{0,40}?):\s+(.*)$`)

// secretKey is a field name that says its value is a secret.
var secretKey = regexp.MustCompile(`(?i)pass|pin\b|secret|token|key|cvv|cvc|recovery|backup.?code|security.?answer`)

// Parse reads a decrypted entry.
func Parse(text string) Entry {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var e Entry
	if len(lines) == 0 {
		return e
	}
	e.Password = lines[0]
	var notes []string
	for _, l := range lines[1:] {
		t := strings.TrimSpace(l)
		switch m := fieldLine.FindStringSubmatch(l); {
		case len(t) >= 3 && strings.Trim(t, "-") == "":
			// A divider ("---"), as gopass and browserpass write before
			// the fields: neither a field nor a note.
		case strings.HasPrefix(t, "otpauth://") && e.OTP == "":
			e.OTP = t
		case m != nil && !strings.Contains(m[1], "://"):
			key := strings.TrimSpace(m[1])
			if strings.EqualFold(key, "otpauth") {
				notes = append(notes, l)
				continue
			}
			e.Fields = append(e.Fields, Field{Key: key, Value: m[2], Secret: secretKey.MatchString(key)})
		default:
			notes = append(notes, l)
		}
	}
	e.Notes = strings.Trim(strings.Join(notes, "\n"), "\n")
	return e
}

// OTP is a time-based one-time password, from an otpauth://totp/ URI.
type OTP struct {
	Label  string
	secret []byte
	digits int
	period time.Duration
	hash   func() hash.Hash
}

// ParseOTP reads an otpauth://totp/ URI. HOTP (counter-based) URIs are
// refused: their counter would have to be written back on each use.
func ParseOTP(uri string) (OTP, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "otpauth" {
		return OTP{}, errors.New(i18n.T("Not an otpauth:// link."))
	}
	if !strings.EqualFold(u.Host, "totp") {
		return OTP{}, errors.New(i18n.Tf("Only time-based codes (totp), not %s.", u.Host))
	}
	q := u.Query()
	o := OTP{Label: strings.TrimPrefix(u.Path, "/"), digits: 6, period: 30 * time.Second, hash: sha1.New}
	secret := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(q.Get("secret")))
	o.secret, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(secret, "="))
	if err != nil || len(o.secret) == 0 {
		return OTP{}, errors.New(i18n.T("The secret is letters A to Z and digits 2 to 7 (base32)."))
	}
	if d := q.Get("digits"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 6 || n > 8 {
			return OTP{}, errors.New(i18n.Tf("Digits %s: want 6 to 8.", d))
		}
		o.digits = n
	}
	if p := q.Get("period"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			return OTP{}, errors.New(i18n.Tf("Period %s: want seconds.", p))
		}
		o.period = time.Duration(n) * time.Second
	}
	switch strings.ToUpper(q.Get("algorithm")) {
	case "", "SHA1":
	case "SHA256":
		o.hash = sha256.New
	case "SHA512":
		o.hash = sha512.New
	default:
		return OTP{}, errors.New(i18n.Tf("Algorithm %s: want SHA1, SHA256 or SHA512.", q.Get("algorithm")))
	}
	return o, nil
}

// Code is the code at t (RFC 6238), and how long it stays.
func (o OTP) Code(t time.Time) (code string, left time.Duration) {
	step := t.Unix() / int64(o.period/time.Second)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(o.hash, o.secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range o.digits {
		mod *= 10
	}
	next := time.Unix((step+1)*int64(o.period/time.Second), 0)
	return fmt.Sprintf("%0*d", o.digits, v%mod), next.Sub(t)
}
