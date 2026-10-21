package mail

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestVerified(t *testing.T) {
	for _, c := range []struct {
		results string
		ok      bool
	}{
		{"mx.google.com; dkim=pass header.i=@github.com; spf=pass smtp.mailfrom=github.com; dmarc=pass (p=REJECT sp=REJECT dis=NONE) header.from=github.com", true},
		{"spf=pass smtp.mailfrom=github.com; dkim=pass header.d=github.com;dmarc=pass action=none header.from=github.com;compauth=pass reason=100", true},
		{"mx.google.com; dmarc=fail (p=REJECT) header.from=github.com", false},
		{"mx.google.com; dmarc=pass (p=NONE) header.from=evil.example", false}, // another domain's pass
		{"mx.google.com; spf=pass smtp.mailfrom=github.com", false},
		{"", false},
	} {
		if got := Verified(c.results, "github.com"); got != c.ok {
			t.Errorf("%q: %v", c.results, got)
		}
	}
}

// bimiNet answers DNS from records and serves logos over https.
func bimiNet(t *testing.T, records map[string]string, logos map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svg, ok := logos[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(svg))
	}))
	t.Cleanup(srv.Close)
	oldTXT, oldClient := lookupTXT, bimiClient
	t.Cleanup(func() { lookupTXT, bimiClient = oldTXT, oldClient })
	bimiClient = srv.Client()
	lookupTXT = func(_ context.Context, name string) ([]string, error) {
		rec, ok := records[name]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
		}
		return []string{strings.ReplaceAll(rec, "{srv}", srv.URL)}, nil
	}
	return srv
}

const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" version="1.2" baseProfile="tiny-ps" viewBox="0 0 64 64"><title>Brand</title><rect width="64" height="64" fill="#123456"/></svg>`

func TestBrandLogo(t *testing.T) {
	bimiNet(t, map[string]string{
		"_dmarc.brand.example":           "v=DMARC1; p=reject; rua=mailto:d@brand.example",
		"default._bimi.brand.example":    "v=BIMI1; l={srv}/logo.svg; a=",
		"_dmarc.lax.example":             "v=DMARC1; p=none",
		"default._bimi.lax.example":      "v=BIMI1; l={srv}/logo.svg",
		"_dmarc.bad.example":             "v=DMARC1; p=quarantine",
		"default._bimi.bad.example":      "v=BIMI1; l={srv}/bad.svg",
		"_dmarc.declines.example":        "v=DMARC1; p=reject",
		"default._bimi.declines.example": "v=BIMI1; l=",
	}, map[string]string{
		"/logo.svg": logoSVG,
		"/bad.svg":  `<svg><script>alert(1)</script></svg>`,
	})
	ctx := context.Background()
	s := StoreAt(t.TempDir())
	path, err := s.BrandLogo(ctx, "brand.example")
	if err != nil || path == "" {
		t.Fatalf("logo %q, %v", path, err)
	}
	if b, _ := os.ReadFile(path); string(b) != logoSVG {
		t.Fatalf("kept %q", b)
	}
	// A subdomain takes its organisation's record and policy.
	if path, err := s.BrandLogo(ctx, "mail.brand.example"); err != nil || path == "" {
		t.Fatalf("subdomain %q, %v", path, err)
	}
	for _, d := range []string{"lax.example", "bad.example", "declines.example", "nothing.example"} {
		if path, err := s.BrandLogo(ctx, d); err != nil || path != "" {
			t.Errorf("%s: %q, %v", d, path, err)
		}
	}
	// Kept: neither the logo nor its absence is asked again.
	lookupTXT = func(context.Context, string) ([]string, error) {
		t.Fatal("asked again")
		return nil, nil
	}
	if path, _ := s.BrandLogo(ctx, "brand.example"); path == "" {
		t.Fatal("the kept logo is gone")
	}
	if path, _ := s.BrandLogo(ctx, "lax.example"); path != "" {
		t.Fatal("a kept absence is a logo")
	}
	// An error is not kept as an absence.
	s2 := StoreAt(t.TempDir())
	lookupTXT = func(context.Context, string) ([]string, error) {
		return nil, &net.DNSError{Err: "timeout", IsTimeout: true}
	}
	if _, err := s2.BrandLogo(ctx, "brand.example"); err == nil {
		t.Fatal("a DNS timeout said nothing")
	}
	if _, err := os.Stat(s2.dir + "/bimi/brand.example.none"); err == nil {
		t.Fatal("a DNS timeout kept as no logo")
	}
}
