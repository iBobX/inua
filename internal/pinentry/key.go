package pinentry

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Key is the OpenPGP key a passphrase unlocks, as gpg lists it: found by
// the keygrip gpg-agent gives the pinentry (SETKEYINFO), so the dialog
// can show it in parts instead of the agent's sentence, whatever its
// language.
type Key struct {
	Name    string `json:"name,omitempty"` // the primary user ID's parts
	Email   string `json:"email,omitempty"`
	Comment string `json:"comment,omitempty"`
	Algo    string `json:"algo,omitempty"` // "ed25519", "cv25519", "RSA 3072"
	// Fingerprint is the unlocked key's, 40 hex digits (64 for v5).
	Fingerprint string    `json:"fingerprint,omitempty"`
	Created     time.Time `json:"created"`
	Expires     time.Time `json:"expires"` // zero when it does not
	// Subkey is set for a subkey; Primary is then its primary key's
	// fingerprint.
	Subkey  bool   `json:"subkey,omitempty"`
	Primary string `json:"primary,omitempty"`
	Use     string `json:"use,omitempty"` // what the key does: "e" encrypt, "s" sign, "a" authenticate, "c" certify
}

// keyLookupTime is how long LookupKey waits for gpg.
const keyLookupTime = 3 * time.Second

// LookupKey finds the key with keygrip grip in gpg's public keyring. It
// reads only the public keys, so it never waits on the agent asking.
func LookupKey(grip string) (Key, bool) {
	if grip == "" {
		return Key{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), keyLookupTime)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gpg", "--batch", "--no-tty", "--no-auto-check-trustdb",
		"--with-colons", "--with-keygrip", "--fixed-list-mode", "--list-keys", "&"+grip).Output()
	if err != nil && len(out) == 0 {
		return Key{}, false
	}
	return parseKeys(out, grip)
}

// parseKeys finds the key with keygrip grip in gpg's --with-colons
// listing (with --with-keygrip and --fixed-list-mode).
func parseKeys(listing []byte, grip string) (Key, bool) {
	var (
		cur, primary  Key  // the key being read, and its primary key
		found, inPrim bool // grip matched; cur is a primary key
	)
	sc := bufio.NewScanner(bytes.NewReader(listing))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		field := func(n int) string {
			if n < len(f) {
				return f[n]
			}
			return ""
		}
		switch field(0) {
		case "pub", "sub":
			if found {
				return finish(cur, primary), true
			}
			cur = Key{
				Algo:    algo(field(3), field(2), field(16)),
				Created: unix(field(5)),
				Expires: unix(field(6)),
				Use:     strings.Map(lower, field(11)),
				Subkey:  field(0) == "sub",
			}
			inPrim = field(0) == "pub"
			if inPrim {
				primary = Key{}
			}
		case "fpr":
			if cur.Fingerprint == "" {
				cur.Fingerprint = field(9)
			}
			if inPrim && primary.Fingerprint == "" {
				primary = cur
			}
		case "grp":
			if strings.EqualFold(field(9), grip) {
				found = true
			}
		case "uid":
			if primary.Name == "" && primary.Email == "" && field(1) != "r" && field(1) != "e" {
				primary.Name, primary.Comment, primary.Email = splitUserID(unescapeColons(field(9)))
			}
		}
	}
	if found {
		return finish(cur, primary), true
	}
	return Key{}, false
}

// finish gives a key its primary key's user ID and fingerprint.
func finish(k, primary Key) Key {
	k.Name, k.Comment, k.Email = primary.Name, primary.Comment, primary.Email
	if k.Subkey {
		k.Primary = primary.Fingerprint
	}
	return k
}

// lower keeps a key's own capabilities: the lower-case letters (the
// upper-case ones are the whole key's).
func lower(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r
	}
	return -1
}

// algo names a key's algorithm: the curve for an elliptic one, else the
// algorithm and its size.
func algo(id, bits, curve string) string {
	if curve != "" {
		return curve
	}
	name := map[string]string{"1": "RSA", "2": "RSA", "3": "RSA", "16": "ElGamal", "17": "DSA", "18": "ECDH", "19": "ECDSA", "22": "EdDSA"}[id]
	switch {
	case name == "":
		return ""
	case bits != "":
		return name + " " + bits
	}
	return name
}

func unix(s string) time.Time {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0)
	}
	return time.Time{}
}

// unescapeColons undoes gpg's \xNN escapes in a --with-colons field.
func unescapeColons(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitUserID splits "Name (Comment) <email>"; a user ID that is only an
// address is the email.
func splitUserID(uid string) (name, comment, email string) {
	uid = strings.TrimSpace(uid)
	if i := strings.LastIndexByte(uid, '<'); i >= 0 && strings.HasSuffix(uid, ">") {
		email, uid = uid[i+1:len(uid)-1], strings.TrimSpace(uid[:i])
	} else if strings.Contains(uid, "@") && !strings.Contains(uid, " ") {
		return "", "", uid
	}
	if i := strings.IndexByte(uid, '('); i >= 0 && strings.HasSuffix(uid, ")") {
		comment, uid = uid[i+1:len(uid)-1], strings.TrimSpace(uid[:i])
	}
	return uid, comment, email
}

// Grouped writes a fingerprint in groups of four, the halves apart:
// "E515 85CE 69B3 79C6 36C7  D764 977E 5858 B6E4 C43D".
func Grouped(fpr string) string {
	var b strings.Builder
	for i := 0; i < len(fpr); i += 4 {
		switch {
		case i == 0:
		case i == len(fpr)/2 && len(fpr)%8 == 0:
			b.WriteString("  ")
		default:
			b.WriteByte(' ')
		}
		b.WriteString(fpr[i:min(i+4, len(fpr))])
	}
	return b.String()
}

// private finds what gpg-agent's description says of the key that is
// private: the quoted user ID, an address, a key ID or fingerprint.
var private = regexp.MustCompile(`"[^"\n]*"|“[^”\n]*”|[^\s<>()"]+@[^\s<>()",]+|\b(?:0x)?[0-9A-Fa-f]{8,}\b`)

// HasPrivate reports whether a description names the key's owner or ID.
func HasPrivate(desc string) bool { return private.MatchString(desc) }

// MaskPrivate puts dots for what a description says of the key that is
// private, keeping the quotes around a user ID.
func MaskPrivate(desc, dots string) string {
	return private.ReplaceAllStringFunc(desc, func(m string) string {
		switch {
		case strings.HasPrefix(m, `"`):
			return `"` + dots + `"`
		case strings.HasPrefix(m, "“"):
			return "“" + dots + "”"
		}
		return dots
	})
}
