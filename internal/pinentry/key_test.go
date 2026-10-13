package pinentry

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// listing is gpg's --with-colons --with-keygrip --fixed-list-mode output
// for a key with an encryption subkey.
const listing = `tru::1:1791882018:0:3:1:5
pub:u:255:22:977E5858B6E4C43D:1791882014:1886490014::u:::scESC:::::ed25519:::0:
fpr:::::::::E51585CE69B379C636C7D764977E5858B6E4C43D:
grp:::::::::63F4B1C9F5ADB987DA240D1B5C00C35B0A1F9397:
uid:r::::1791880000::0000::Old Name <old@example.org>::::::::::0:
uid:u::::1791882014::3EE9::Test Key (work\x3a laptop) <test@example.org>::::::::::0:
sub:u:3072:1:AFC95841F7D506CF:1791882014::::::e::::::
fpr:::::::::B5EE3BE1693E32ACDAFEE470AFC95841F7D506CF:
grp:::::::::08CC84D586F6B0DEFF0BB6B42204A2957B5CCA90:
pub:u:255:22:1111111111111111:1791882014:::u:::scSC:::::ed25519:::0:
fpr:::::::::2222222222222222222222221111111111111111:
grp:::::::::3333333333333333333333333333333333333333:
uid:u::::1791882014::4444::alone@example.org::::::::::0:
`

func TestParseKeys(t *testing.T) {
	sub, ok := parseKeys([]byte(listing), "08cc84d586f6b0deff0bb6b42204a2957b5cca90")
	if !ok {
		t.Fatal("subkey not found")
	}
	want := Key{
		Name: "Test Key", Comment: "work: laptop", Email: "test@example.org",
		Algo: "RSA 3072", Fingerprint: "B5EE3BE1693E32ACDAFEE470AFC95841F7D506CF",
		Created: time.Unix(1791882014, 0), Subkey: true,
		Primary: "E51585CE69B379C636C7D764977E5858B6E4C43D", Use: "e",
	}
	if sub != want {
		t.Errorf("subkey\n got %+v\nwant %+v", sub, want)
	}

	prim, ok := parseKeys([]byte(listing), "63F4B1C9F5ADB987DA240D1B5C00C35B0A1F9397")
	if !ok || prim.Subkey || prim.Primary != "" || prim.Algo != "ed25519" || prim.Use != "sc" ||
		prim.Name != "Test Key" || !prim.Expires.Equal(time.Unix(1886490014, 0)) {
		t.Errorf("primary key: %+v", prim)
	}
	last, ok := parseKeys([]byte(listing), "3333333333333333333333333333333333333333")
	if !ok || last.Email != "alone@example.org" || last.Name != "" {
		t.Errorf("an address alone: %+v", last)
	}
	if _, ok := parseKeys([]byte(listing), "FFFF"); ok {
		t.Error("an unknown keygrip found")
	}
}

func TestGrouped(t *testing.T) {
	if got := Grouped("E51585CE69B379C636C7D764977E5858B6E4C43D"); got != "E515 85CE 69B3 79C6 36C7  D764 977E 5858 B6E4 C43D" {
		t.Errorf("Grouped = %q", got)
	}
}

func TestMaskPrivate(t *testing.T) {
	desc := "Please enter the passphrase to unlock the OpenPGP secret key:\n" +
		`"Test Key <test@example.org>"` + "\n3072-bit RSA key, ID AFC95841F7D506CF,\ncreated 2026-10-13 (main key ID 977E5858B6E4C43D).\n"
	if !HasPrivate(desc) {
		t.Fatal("HasPrivate: the user ID missed")
	}
	got := MaskPrivate(desc, "••")
	for _, leak := range []string{"Test Key", "test@example.org", "AFC95841F7D506CF", "977E5858B6E4C43D"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q left in %q", leak, got)
		}
	}
	if !strings.Contains(got, "3072-bit RSA key") || !strings.Contains(got, "created 2026-10-13") || !strings.Contains(got, `"••"`) {
		t.Errorf("MaskPrivate = %q", got)
	}
	if HasPrivate("Please enter the PIN") {
		t.Error("HasPrivate: nothing private there")
	}
}

func TestSetKeyInfo(t *testing.T) {
	script := "SETKEYINFO n/08CC84D586F6B0DEFF0BB6B42204A2957B5CCA90\nGETPIN\nSETKEYINFO --clear\nGETPIN\nBYE\n"
	_, asked := session(t, script, func(Request) (Answer, error) { return Answer{OK: true}, nil })
	if len(asked) != 2 || asked[0].KeyGrip != "08CC84D586F6B0DEFF0BB6B42204A2957B5CCA90" || asked[1].KeyGrip != "" {
		t.Errorf("keygrips %+v", asked)
	}
}

// TestLookupKey makes a key in a throwaway gpg home and finds its
// subkey by keygrip, as the pinentry does.
func TestLookupKey(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("no gpg")
	}
	home := t.TempDir()
	t.Setenv("GNUPGHOME", home)
	gen := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "",
		"--quick-generate-key", "Lookup Test <lookup@example.org>", "ed25519", "sign,cert", "never")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("gpg cannot make a key here: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run() })
	out, err := exec.Command("gpg", "--batch", "--with-colons", "--with-keygrip", "--list-keys").Output()
	if err != nil {
		t.Fatal(err)
	}
	var grip string
	for line := range strings.SplitSeq(string(out), "\n") {
		if f := strings.Split(line, ":"); f[0] == "grp" && grip == "" {
			grip = f[9]
		}
	}
	k, ok := LookupKey(grip)
	if !ok || k.Name != "Lookup Test" || k.Email != "lookup@example.org" || k.Algo != "ed25519" || len(k.Fingerprint) != 40 {
		t.Errorf("LookupKey(%s) = %+v, %v", grip, k, ok)
	}
}
