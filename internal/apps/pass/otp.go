package pass

import (
	"encoding/base32"
	"errors"
	"image"
	"net/url"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/makiuchi-d/gozxing"
	multiqr "github.com/makiuchi-d/gozxing/multi/qrcode"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// Adding a one-time code (docs/feat/pass-otp.md): from a secret typed or
// pasted, an otpauth:// link, or a QR code read from the screen or an
// image.

// OTPURI makes the otpauth:// line for what the user gave: a link, kept
// as it is but for the issuer and the label it lacks; or a secret (base32;
// spaces, dashes and lower case are fine), a time-based code of 6 digits
// every 30 seconds, as sites give. It errs when the code would not read.
func OTPURI(input, issuer, account string) (string, error) {
	input = strings.TrimSpace(input)
	issuer, account = strings.TrimSpace(issuer), strings.TrimSpace(account)
	if input == "" {
		return "", errors.New(i18n.T("Type or paste the secret, or read its QR code."))
	}
	if strings.HasPrefix(strings.ToLower(input), "otpauth-migration:") {
		return "", errMigration()
	}
	var u *url.URL
	if strings.HasPrefix(strings.ToLower(input), "otpauth:") {
		var err error
		if u, err = url.Parse(input); err != nil {
			return "", errors.New(i18n.T("Not an otpauth:// link."))
		}
	} else {
		secret := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(input))
		secret = strings.TrimRight(secret, "=")
		if b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret); err != nil || len(b) == 0 {
			return "", errors.New(i18n.T("The secret is letters A to Z and digits 2 to 7 (base32)."))
		}
		u = &url.URL{Scheme: "otpauth", Host: "totp", RawQuery: url.Values{"secret": {secret}}.Encode()}
	}
	q := u.Query()
	if q.Get("issuer") == "" && issuer != "" {
		q.Set("issuer", issuer)
		u.RawQuery = q.Encode()
	}
	if strings.Trim(u.Path, "/") == "" {
		label := account
		if iss := q.Get("issuer"); iss != "" && account != "" {
			label = iss + ":" + account
		} else if label == "" {
			label = iss
		}
		if label != "" {
			u.Path = "/" + label
		}
	}
	uri := u.String()
	if _, err := ParseOTP(uri); err != nil {
		return "", err
	}
	return uri, nil
}

// OTPLabel is the issuer and the account a link names, for the fields
// that show them.
func OTPLabel(uri string) (issuer, account string) {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return "", ""
	}
	issuer = u.Query().Get("issuer")
	label := strings.Trim(u.Path, "/")
	if i, a, ok := strings.Cut(label, ":"); ok {
		return strings.TrimSpace(cmpOr(issuer, i)), strings.TrimSpace(a)
	}
	return issuer, label
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// WithOTP puts uri in an entry's text: in place of its otpauth:// line,
// else after the password (the first line).
func WithOTP(content, uri string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if i > 0 && (strings.HasPrefix(t, "otpauth://") || strings.HasPrefix(strings.ToLower(t), "otpauth:")) {
			lines[i] = uri
			return strings.Join(lines, "\n") + "\n"
		}
	}
	out := append([]string{lines[0], uri}, lines[1:]...)
	return strings.Join(out, "\n") + "\n"
}

// ReadQR reads the QR codes in an image: their texts, none when there
// is none.
func ReadQR(img image.Image) []string {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil
	}
	hints := map[gozxing.DecodeHintType]any{gozxing.DecodeHintType_TRY_HARDER: true}
	var out []string
	add := func(s string) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if rs, err := multiqr.NewQRCodeMultiReader().DecodeMultiple(bmp, hints); err == nil {
		for _, r := range rs {
			add(r.GetText())
		}
	}
	if len(out) == 0 {
		if r, err := qrcode.NewQRCodeReader().Decode(bmp, hints); err == nil {
			add(r.GetText())
		}
	}
	return out
}

// errMigration refuses Google Authenticator's export.
func errMigration() error {
	return errors.New(i18n.T("This is a Google Authenticator export, which holds several accounts; show each site's own QR code instead."))
}

// OTPFromQR picks the one-time code among QR codes' texts.
func OTPFromQR(texts []string) (string, error) {
	var bad error
	for _, t := range texts {
		t = strings.TrimSpace(t)
		switch lower := strings.ToLower(t); {
		case strings.HasPrefix(lower, "otpauth://"):
			if _, err := ParseOTP(t); err != nil {
				bad = err
				continue
			}
			return t, nil
		case strings.HasPrefix(lower, "otpauth-migration:"):
			bad = errMigration()
		}
	}
	switch {
	case bad != nil:
		return "", bad
	case len(texts) == 0:
		return "", errors.New(i18n.T("No QR code found."))
	}
	return "", errors.New(i18n.T("The QR code is not a one-time code."))
}
