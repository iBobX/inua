package pass

import (
	"encoding/base32"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	e := Parse("hunter2\nuser: ann\nurl: https://example.org/login\nPIN: 1234\notpauth://totp/Ex:ann?secret=JBSWY3DPEHPK3PXP\nfree text: is not a field? no\n\nremember the dog\n")
	if e.Password != "hunter2" || e.OTP != "otpauth://totp/Ex:ann?secret=JBSWY3DPEHPK3PXP" {
		t.Errorf("password %q otp %q", e.Password, e.OTP)
	}
	want := []Field{{"user", "ann", false}, {"url", "https://example.org/login", false}, {"PIN", "1234", true}, {"free text", "is not a field? no", false}}
	if !slices.Equal(e.Fields, want) {
		t.Errorf("fields %+v", e.Fields)
	}
	if e.Notes != "remember the dog" {
		t.Errorf("notes %q", e.Notes)
	}
	if e := Parse("only\n"); e.Password != "only" || e.Fields != nil || e.Notes != "" {
		t.Errorf("password alone: %+v", e)
	}
	// A divider is neither a field nor a note.
	if e := Parse("p\n---\nuser: ann\n-----\nkept\n"); len(e.Fields) != 1 || e.Notes != "kept" {
		t.Errorf("dividers: %+v", e)
	}
	// A URL alone on a line is a note, not a field "https".
	if e := Parse("p\nhttps://example.org\n"); len(e.Fields) != 0 || e.Notes != "https://example.org" {
		t.Errorf("a URL: %+v", e)
	}
}

// TestOTP checks the codes against RFC 6238's test vectors.
func TestOTP(t *testing.T) {
	b32 := func(s string) string {
		return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(s))
	}
	cases := []struct {
		alg, secret string
		at          int64
		want        string
	}{
		{"SHA1", "12345678901234567890", 59, "94287082"},
		{"SHA1", "12345678901234567890", 1111111109, "07081804"},
		{"SHA256", "12345678901234567890123456789012", 59, "46119246"},
		{"SHA512", "1234567890123456789012345678901234567890123456789012345678901234", 1234567890, "93441116"},
		{"SHA1", "12345678901234567890", 20000000000, "65353130"},
	}
	for _, c := range cases {
		o, err := ParseOTP("otpauth://totp/Test?secret=" + b32(c.secret) + "&digits=8&algorithm=" + c.alg)
		if err != nil {
			t.Fatal(err)
		}
		if code, _ := o.Code(time.Unix(c.at, 0)); code != c.want {
			t.Errorf("%s at %d: %s, want %s", c.alg, c.at, code, c.want)
		}
	}
	o, err := ParseOTP("otpauth://totp/Ex:ann?secret=jbsw y3dp-ehpk3pxp&issuer=Ex")
	if err != nil {
		t.Fatal(err)
	}
	if code, left := o.Code(time.Unix(65, 0)); len(code) != 6 || left != 25*time.Second {
		t.Errorf("defaults: %q, %v left", code, left)
	}
	for _, bad := range []string{"otpauth://hotp/x?secret=JBSWY3DP&counter=1", "otpauth://totp/x?secret=!!", "https://x", "otpauth://totp/x?secret=JBSWY3DP&digits=4"} {
		if _, err := ParseOTP(bad); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

func TestCategories(t *testing.T) {
	entries := []string{"top", "work/mail/ann", "work/vpn", "home/wifi", "inua-keyring/login/x"}
	got := Categories(entries)
	want := []Category{{"", 1}, {"home", 1}, {"inua-keyring", 1}, {"work", 2}}
	if !slices.Equal(got, want) {
		t.Errorf("categories %+v", got)
	}
	if got := Subpaths(entries, "work"); !slices.Equal(got, []string{"mail/ann", "vpn"}) {
		t.Errorf("work: %q", got)
	}
	if got := Subpaths(entries, ""); !slices.Equal(got, []string{"top"}) {
		t.Errorf("top level: %q", got)
	}
	if c, s := Split("work/mail/ann"); c != "work" || s != "mail/ann" || Join(c, s) != "work/mail/ann" || Join("", "top") != "top" {
		t.Errorf("split %q %q", c, s)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "a/b", "work/mail/ann", "名前/x"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "/a", "a/", "a//b", "../a", "a/./b", ".git", "a/.hidden"} {
		if ValidName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestStores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	env := filepath.Join(home, "env-store")
	added := filepath.Join(home, "added")
	notStore := filepath.Join(home, "plain")
	for _, d := range []string{env, added, filepath.Join(home, ".password-store")} {
		os.MkdirAll(d, 0o700)
		os.WriteFile(filepath.Join(d, ".gpg-id"), []byte("ID\n"), 0o600)
	}
	os.MkdirAll(notStore, 0o700)
	t.Setenv("PASSWORD_STORE_DIR", env)
	p := Prefs{Added: []string{added, notStore, env}}
	got := Stores(p)
	if want := []string{env, filepath.Join(home, ".password-store"), added}; !slices.Equal(got, want) {
		t.Errorf("stores %q", got)
	}
	if Current(Prefs{Store: added}, got) != added || Current(Prefs{Store: "/gone"}, got) != env || Current(Prefs{}, nil) != "" {
		t.Error("Current")
	}
}

// TestStore runs the real pass and gpg in a temporary home: insert, list,
// show, move an entry and a folder, refuse a move onto an entry, remove.
func TestStore(t *testing.T) {
	for _, p := range []string{"pass", "gpg", "gpgconf"} {
		if _, err := exec.LookPath(p); err != nil {
			t.Skip("no " + p)
		}
	}
	home := t.TempDir()
	gnupg := filepath.Join(home, "gnupg")
	os.MkdirAll(gnupg, 0o700)
	t.Setenv("GNUPGHOME", gnupg)
	t.Setenv("HOME", home)
	t.Cleanup(func() { exec.Command("gpgconf", "--homedir", gnupg, "--kill", "all").Run() })
	gen := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "",
		"--quick-generate-key", "Pass Test <pass@example.org>", "default", "default", "never")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("gpg cannot make a key here: %v %s", err, out)
	}
	dir := filepath.Join(home, "store")
	init := exec.Command("pass", "init", "pass@example.org")
	init.Env = append(os.Environ(), "PASSWORD_STORE_DIR="+dir)
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("pass init: %v %s", err, out)
	}
	s := Store{Dir: dir}
	content := "hunter2\nuser: ann\n"
	for _, name := range []string{"work/mail/ann", "work/vpn", "top"} {
		if err := s.Insert(name, content); err != nil {
			t.Fatal(err)
		}
	}
	names, err := s.Entries()
	if err != nil || !slices.Equal(names, []string{"top", "work/mail/ann", "work/vpn"}) {
		t.Fatalf("entries %q %v", names, err)
	}
	if got, err := s.Show("work/vpn"); err != nil || got != content {
		t.Errorf("show: %q %v", got, err)
	}
	if err := s.Move("work/vpn", "office/vpn"); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("work", "archive/work"); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("top", "office/vpn"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("moving onto an entry: %v", err)
	}
	if err := s.Remove("top"); err != nil {
		t.Fatal(err)
	}
	names, _ = s.Entries()
	if !slices.Equal(names, []string{"archive/work/mail/ann", "office/vpn"}) {
		t.Errorf("after moves: %q", names)
	}
	if _, err := s.Show("nothing/here"); err == nil {
		t.Error("show of a missing entry")
	}
}

func TestShortNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := ShortNames([]string{filepath.Join(home, ".password-store"), "/srv/team/store", "/mnt/usb/store"})
	if want := []string{"~/.password-store", "team/store", "usb/store"}; !slices.Equal(got, want) {
		t.Errorf("ShortNames = %q", got)
	}
	if got := ShortNames([]string{filepath.Join(home, "a/store"), "/b/store"}); !slices.Equal(got, []string{"a/store", "b/store"}) {
		t.Errorf("two stores: %q", got)
	}
}

func TestTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := Tilde(filepath.Join(home, ".password-store")); got != "~/.password-store" {
		t.Errorf("Tilde = %q", got)
	}
	if got := Tilde("/srv/store"); got != "/srv/store" {
		t.Errorf("outside home: %q", got)
	}
}

func TestPlanMove(t *testing.T) {
	entries := []string{"top", "work/github", "work/servers/db", "home/wifi"}
	cases := []struct {
		from, to string
		want     []Move
		err      string
	}{
		{"", "work/gitlab", []Move{{"", "work/gitlab"}}, ""},
		{"", " /new/entry/ ", []Move{{"", "new/entry"}}, ""},
		{"work/github", "office/github", []Move{{"work/github", "office/github"}}, ""},
		{"work", "archive/work", []Move{{"work/github", "archive/work/github"}, {"work/servers/db", "archive/work/servers/db"}}, ""},
		{"top", "home/top", []Move{{"top", "home/top"}}, ""},
		{"work/github", "work/github", nil, "where it is"},
		{"work", "work/old", nil, "into itself"},
		{"work/github", "home/wifi", nil, "already"},
		{"top", "home", nil, "already"}, // a folder of that name
		{"", "", nil, "empty"},
		{"", "a/../b", nil, "dot"},
		{"gone", "x", nil, "not in the store"},
	}
	for _, c := range cases {
		got, err := PlanMove(entries, c.from, c.to)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("PlanMove(%q, %q) error %v, want %q", c.from, c.to, err, c.err)
			}
			continue
		}
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("PlanMove(%q, %q) = %v, %v", c.from, c.to, got, err)
		}
	}
}

func TestIcon(t *testing.T) {
	cases := map[string]string{
		"work/github":            "github-logo",
		"personal/GitHub.com":    "github-logo",
		"dev/github/ann":         "github-logo", // the folder names the service
		"google/ann@example.org": "google-logo", // the category, a service
		"home/wifi":              "wifi-high",
		"home/router":            "network",
		"personal/bank":          "bank",
		"personal/visa-card":     "credit-card",
		"work/servers/db/prod":   "hard-drives", // the leaf "prod"
		"work/servers/db":        "database",
		"work/ssh-bastion":       "terminal-window",
		"dev/npm-token":          "package", // npm before token
		"dev/api-key":            "key",
		"work/vpn":               "shield-check",
		"x":                      "x-logo",
		"personal/email":         "envelope",
		"work/aws-console":       "cloud",
		"home/notes":             "", // nothing says
		"work/mail/ann":          "envelope",
		"work/ann":               "", // "work" is a category: no kind from it
		"misc/boxer":             "", // a word, not a part of one: no "box"
	}
	for name, want := range cases {
		if got := Icon(name); got != want {
			t.Errorf("Icon(%q) = %q, want %q", name, got, want)
		}
	}
	for _, icon := range Icons() {
		if icon == "" || strings.ContainsAny(icon, " /") {
			t.Errorf("Icons: %q", icon)
		}
	}
}
