package mail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// BIMI (Brand Indicators for Message Identification): a domain publishes
// its logo in DNS, default._bimi.<domain>, and Mail shows it as the face
// of a message proven to come from that domain: the user's provider found
// DMARC passing for the From's domain (Verified), and the domain's DMARC
// policy quarantines or rejects what fails, as BIMI asks. The logo is an
// SVG kept a week; a domain with none is asked again after a day. The
// certificate some readers ask for (a VMC) is not checked.

var (
	dmarcResult = regexp.MustCompile(`(?i)\bdmarc\s*=\s*([a-z]+)([^;]*)`)
	headerFrom  = regexp.MustCompile(`(?i)\bheader\.from\s*=\s*([^\s;()]+)`)
)

// Verified says whether results, the provider's Authentication-Results,
// prove a message from domain: DMARC passed, for that domain.
func Verified(results, domain string) bool {
	m := dmarcResult.FindStringSubmatch(results)
	if m == nil || !strings.EqualFold(m[1], "pass") {
		return false
	}
	f := headerFrom.FindStringSubmatch(m[2])
	return f != nil && domain != "" && strings.EqualFold(strings.TrimSuffix(f[1], "."), domain)
}

// Domain is the domain of an address, lower case; "" when it has none.
func Domain(addr string) string {
	_, d, ok := strings.Cut(strings.TrimSpace(addr), "@")
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(d, "."))
}

// The network BIMI asks; tests replace them.
var (
	lookupTXT  = net.DefaultResolver.LookupTXT
	bimiClient = &http.Client{Timeout: 15 * time.Second}
)

const (
	bimiKeep = 7 * 24 * time.Hour // a logo
	bimiNone = 24 * time.Hour     // a domain without one
	bimiMax  = 64 << 10           // BIMI asks for 32 KB at most
)

// BrandLogo is the path of domain's BIMI logo (an SVG) in the cache,
// asked of DNS and fetched when it is not kept: "" when the domain has
// none, or its DMARC policy lets failing mail through. An error (the
// network) is not kept: the next call asks again.
func (s *Store) BrandLogo(ctx context.Context, domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if domain == "" {
		return "", nil
	}
	dir := filepath.Join(s.dir, "bimi")
	logo := filepath.Join(dir, url.PathEscape(domain)+".svg")
	none := filepath.Join(dir, url.PathEscape(domain)+".none")
	if fresh(logo, bimiKeep) {
		return logo, nil
	}
	if fresh(none, bimiNone) {
		return "", nil
	}
	svg, err := fetchBIMI(ctx, domain)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if svg == nil {
		os.Remove(logo)
		return "", writeFile(none, nil)
	}
	os.Remove(none)
	return logo, writeFile(logo, svg)
}

func fresh(path string, d time.Duration) bool {
	st, err := os.Stat(path)
	return err == nil && time.Since(st.ModTime()) < d
}

// fetchBIMI finds domain's logo: its DMARC policy enforced, its BIMI
// record (else its organisational domain's), and the SVG it names; nil
// when there is none to show.
func fetchBIMI(ctx context.Context, domain string) ([]byte, error) {
	org := domain
	if o, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		org = o
	}
	ok, err := dmarcEnforced(ctx, domain, org)
	if err != nil || !ok {
		return nil, err
	}
	rec, err := txtRecord(ctx, "default._bimi."+domain, "BIMI1")
	if err == nil && rec == nil && org != domain {
		rec, err = txtRecord(ctx, "default._bimi."+org, "BIMI1")
	}
	if err != nil || rec == nil {
		return nil, err
	}
	u, perr := url.Parse(rec["l"])
	if perr != nil || u.Scheme != "https" || u.Host == "" {
		return nil, nil // none named (a domain that declines), or not over https
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil
	}
	resp, err := bimiClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	svg, err := io.ReadAll(io.LimitReader(resp.Body, bimiMax+1))
	if err != nil {
		return nil, err
	}
	if len(svg) > bimiMax || !safeSVG(svg) {
		return nil, nil
	}
	return svg, nil
}

// dmarcEnforced says whether a domain's DMARC policy quarantines or
// rejects all mail that fails it: its own record, else its organisational
// domain's, whose sp= covers its subdomains.
func dmarcEnforced(ctx context.Context, domain, org string) (bool, error) {
	rec, err := txtRecord(ctx, "_dmarc."+domain, "DMARC1")
	policy := "p"
	if err == nil && rec == nil && org != domain {
		rec, err = txtRecord(ctx, "_dmarc."+org, "DMARC1")
		policy = "sp"
		if rec != nil && rec["sp"] == "" {
			policy = "p"
		}
	}
	if err != nil || rec == nil {
		return false, err
	}
	if pct := rec["pct"]; pct != "" && pct != "100" {
		return false, nil
	}
	p := strings.ToLower(rec[policy])
	return p == "quarantine" || p == "reject", nil
}

// txtRecord reads the TXT record at name whose v= is version, as its
// tags (lower-case names); nil when there is none.
func txtRecord(ctx context.Context, name, version string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	txts, err := lookupTXT(ctx, name)
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, t := range txts {
		tags := map[string]string{}
		for _, part := range strings.Split(t, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok {
				tags[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
			}
		}
		if strings.EqualFold(tags["v"], version) {
			return tags, nil
		}
	}
	return nil, nil
}

// safeSVG says whether data is an SVG with nothing BIMI's profile forbids
// (SVG Tiny Portable/Secure): no script, no foreign content, nothing from
// elsewhere.
func safeSVG(data []byte) bool {
	low := bytes.ToLower(data)
	if !bytes.Contains(low, []byte("<svg")) {
		return false
	}
	for _, bad := range []string{"<script", "<foreignobject", "href=\"http", "href='http", "href=\"//", "href='//", "url(http", "url(//", "<!entity"} {
		if bytes.Contains(low, []byte(bad)) {
			return false
		}
	}
	return true
}
