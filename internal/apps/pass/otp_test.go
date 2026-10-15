package pass

import (
	"image"
	"image/color"
	"image/draw"
	"math/rand"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func TestOTPURI(t *testing.T) {
	for _, c := range []struct {
		input, issuer, account string
		want                   string // "" for an error
	}{
		{"jbsw y3dp ehpk 3pxp", "GitHub", "ada", "otpauth://totp/GitHub:ada?issuer=GitHub&secret=JBSWY3DPEHPK3PXP"},
		{"JBSW-Y3DP-EHPK-3PXP====", "", "", "otpauth://totp?secret=JBSWY3DPEHPK3PXP"},
		{"JBSWY3DPEHPK3PXP", "", "ada@example.org", "otpauth://totp/ada@example.org?secret=JBSWY3DPEHPK3PXP"},
		// A link keeps its own label and issuer.
		{"otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP&issuer=Example&digits=8", "GitHub", "ada",
			"otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP&issuer=Example&digits=8"},
		{"otpauth://totp/?secret=JBSWY3DPEHPK3PXP", "GitHub", "ada", "otpauth://totp/GitHub:ada?issuer=GitHub&secret=JBSWY3DPEHPK3PXP"},
		{"", "", "", ""},
		{"not a secret!", "", "", ""},
		{"otpauth://hotp/x?secret=JBSWY3DPEHPK3PXP&counter=1", "", "", ""},
		{"otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&digits=4", "", "", ""},
		{"otpauth-migration://offline?data=abc", "", "", ""},
	} {
		got, err := OTPURI(c.input, c.issuer, c.account)
		switch {
		case c.want == "" && err == nil:
			t.Errorf("%q: %q, want an error", c.input, got)
		case c.want != "" && (err != nil || got != c.want):
			t.Errorf("%q: %q %v, want %q", c.input, got, err, c.want)
		}
	}
	if iss, acc := OTPLabel("otpauth://totp/Example:alice@x.org?secret=A&issuer=Ex"); iss != "Ex" || acc != "alice@x.org" {
		t.Errorf("label %q %q", iss, acc)
	}
}

func TestWithOTP(t *testing.T) {
	const uri = "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP"
	for in, want := range map[string]string{
		"hunter2\nuser: ada\n": "hunter2\n" + uri + "\nuser: ada\n",
		"hunter2":              "hunter2\n" + uri + "\n",
		"hunter2\nuser: ada\notpauth://totp/old?s=1\n":  "hunter2\nuser: ada\n" + uri + "\n",
		"hunter2\notpauth: otpauth://totp/old?secret=1": "hunter2\n" + uri + "\n",
		"": "\n" + uri + "\n",
	} {
		if got := WithOTP(in, uri); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	// What WithOTP writes, Parse reads.
	if e := Parse(WithOTP("hunter2\nuser: ada\n", uri)); e.OTP != uri || e.Password != "hunter2" || len(e.Fields) != 1 {
		t.Errorf("parsed %+v", e)
	}
}

// qrImage draws text's QR code at (x, y) on a w×h image of grey noise,
// as a code on a web page in a screenshot.
func qrImage(t *testing.T, w, h, x, y int, texts ...string) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		if i%4 == 3 {
			img.Pix[i] = 255
		} else {
			img.Pix[i] = uint8(150 + rng.Intn(60))
		}
	}
	for i, text := range texts {
		m, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, 200, 200, nil)
		if err != nil {
			t.Fatal(err)
		}
		at := image.Pt(x+i*260, y)
		draw.Draw(img, image.Rect(at.X, at.Y, at.X+200, at.Y+200), image.NewUniform(color.White), image.Point{}, draw.Src)
		for py := range 200 {
			for px := range 200 {
				if m.Get(px, py) {
					img.Set(at.X+px, at.Y+py, color.Black)
				}
			}
		}
	}
	return img
}

func TestReadQR(t *testing.T) {
	const uri = "otpauth://totp/GitHub:ada?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"
	texts := ReadQR(qrImage(t, 1280, 800, 700, 300, uri))
	got, err := OTPFromQR(texts)
	if err != nil || got != uri {
		t.Fatalf("%q: %q %v", texts, got, err)
	}
	// Among other codes, the one-time code is picked.
	texts = ReadQR(qrImage(t, 1280, 800, 100, 300, "https://example.org", uri))
	if got, err := OTPFromQR(texts); err != nil || got != uri {
		t.Errorf("two codes %q: %q %v", texts, got, err)
	}
	if texts := ReadQR(qrImage(t, 640, 400, 0, 0)); len(texts) != 0 {
		t.Errorf("noise read as %q", texts)
	}
	for _, c := range []struct {
		texts []string
		want  string
	}{
		{nil, "No QR code"},
		{[]string{"https://example.org"}, "not a one-time code"},
		{[]string{"otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8"}, "Google Authenticator"},
		{[]string{"otpauth://hotp/x?secret=JBSWY3DPEHPK3PXP"}, "totp"},
	} {
		if _, err := OTPFromQR(c.texts); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.texts, err, c.want)
		}
	}
}
