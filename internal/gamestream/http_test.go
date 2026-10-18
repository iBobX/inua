package gamestream

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
)

// fakeHost does what Sunshine does over HTTP and HTTPS: its server info,
// the pairing stages (checking the client's proofs as a host does), the
// app list and launching, the latter only for the certificate it paired.
type fakeHost struct {
	t        *testing.T
	id       *Identity // the host's own certificate and key
	pin      string    // what its user types
	plain    *httptest.Server
	secure   *httptest.Server
	mu       sync.Mutex
	salt     []byte
	client   *x509.Certificate
	key      []byte
	sChal    []byte
	sSecret  []byte
	cHash    []byte
	paired   []byte // the paired client's certificate, DER
	launched url.Values
	session  string // the RTSP URL a launch gives
}

func newFakeHost(t *testing.T, pin string) *fakeHost {
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeHost{t: t, id: id, pin: pin}
	f.plain = httptest.NewServer(http.HandlerFunc(f.serve))
	f.secure = httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	f.secure.TLS = &tls.Config{Certificates: []tls.Certificate{id.tlsCertificate()}, ClientAuth: tls.RequireAnyClientCert}
	f.secure.StartTLS()
	t.Cleanup(func() { f.plain.Close(); f.secure.Close() })
	return f
}

func (f *fakeHost) addr() string { return f.plain.Listener.Addr().String() }

func (f *fakeHost) reply(w http.ResponseWriter, code int, body string) {
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><root status_code="%d" status_message="x">%s</root>`, code, body)
}

func (f *fakeHost) serve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("uniqueid") == "" || len(q.Get("uuid")) != 32 {
		f.reply(w, 400, "")
		return
	}
	secure := r.TLS != nil
	var peer []byte
	if secure && len(r.TLS.PeerCertificates) > 0 {
		peer = r.TLS.PeerCertificates[0].Raw
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	paired := peer != nil && bytes.Equal(peer, f.paired)
	switch r.URL.Path {
	case "/serverinfo":
		status := 0
		if paired {
			status = 1
		}
		_, port, _ := net.SplitHostPort(f.secure.Listener.Addr().String())
		f.reply(w, 200, fmt.Sprintf(`<hostname>desk</hostname><appversion>7.1.431.-1</appversion><uniqueid>abc</uniqueid>`+
			`<HttpsPort>%s</HttpsPort><state>SUNSHINE_SERVER_FREE</state><currentgame>0</currentgame>`+
			`<PairStatus>%d</PairStatus><ServerCodecModeSupport>65793</ServerCodecModeSupport>`, port, status))
	case "/pair":
		f.pair(w, q, secure, peer)
	case "/unpair":
		f.paired = nil
		f.reply(w, 200, "")
	case "/applist":
		if !paired {
			w.WriteHeader(401)
			return
		}
		f.reply(w, 200, `<App><AppTitle>Desktop</AppTitle><ID>881448767</ID></App><App><AppTitle>Steam</AppTitle><ID>2</ID></App>`)
	case "/launch":
		if !paired {
			w.WriteHeader(401)
			return
		}
		f.launched = q
		session := f.session
		if session == "" {
			session = "rtsp://127.0.0.1:48010"
		}
		f.reply(w, 200, `<sessionUrl0>`+session+`</sessionUrl0><gamesession>1</gamesession>`)
	default:
		f.reply(w, 404, "")
	}
}

func (f *fakeHost) pair(w http.ResponseWriter, q url.Values, secure bool, peer []byte) {
	fail := func() { f.reply(w, 200, "<paired>0</paired>") }
	switch {
	case q.Get("phrase") == "getservercert":
		f.salt, _ = hex.DecodeString(q.Get("salt"))
		certPEM, _ := hex.DecodeString(q.Get("clientcert"))
		b, _ := pem.Decode(certPEM)
		if b == nil {
			fail()
			return
		}
		f.client, _ = x509.ParseCertificate(b.Bytes)
		sum := sha256.Sum256(append(append([]byte(nil), f.salt...), f.pin...))
		f.key = sum[:16]
		f.reply(w, 200, "<paired>1</paired><plaincert>"+hex.EncodeToString(f.id.CertPEM)+"</plaincert>")
	case q.Has("clientchallenge"):
		enc, _ := hex.DecodeString(q.Get("clientchallenge"))
		challenge := ecb(f.key, enc, false)
		f.sSecret, f.sChal = make([]byte, 16), make([]byte, 16)
		rand.Read(f.sSecret)
		rand.Read(f.sChal)
		hash := sha256.Sum256(concat(challenge, f.id.cert.Signature, f.sSecret))
		f.reply(w, 200, "<paired>1</paired><challengeresponse>"+hex.EncodeToString(ecb(f.key, concat(hash[:], f.sChal), true))+"</challengeresponse>")
	case q.Has("serverchallengeresp"):
		enc, _ := hex.DecodeString(q.Get("serverchallengeresp"))
		f.cHash = ecb(f.key, enc, false)
		digest := sha256.Sum256(f.sSecret)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, f.id.key, crypto.SHA256, digest[:])
		f.reply(w, 200, "<paired>1</paired><pairingsecret>"+hex.EncodeToString(concat(f.sSecret, sig))+"</pairingsecret>")
	case q.Has("clientpairingsecret"):
		b, _ := hex.DecodeString(q.Get("clientpairingsecret"))
		if len(b) <= 16 {
			fail()
			return
		}
		secret, sig := b[:16], b[16:]
		digest := sha256.Sum256(secret)
		if rsa.VerifyPKCS1v15(f.client.PublicKey.(*rsa.PublicKey), crypto.SHA256, digest[:], sig) != nil {
			fail()
			return
		}
		expect := sha256.Sum256(concat(f.sChal, f.client.Signature, secret))
		if !bytes.Equal(expect[:], f.cHash) {
			fail()
			return
		}
		f.paired = f.client.Raw
		f.reply(w, 200, "<paired>1</paired>")
	case q.Get("phrase") == "pairchallenge":
		if !secure || !bytes.Equal(peer, f.paired) {
			fail()
			return
		}
		f.reply(w, 200, "<paired>1</paired>")
	default:
		fail()
	}
}

func newClient(t *testing.T, addr string) *Host {
	id, err := LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Host{Addr: addr, ID: id}
}

func TestPairWithTheRightPIN(t *testing.T) {
	f := newFakeHost(t, "4821")
	h := newClient(t, f.addr())
	info, err := h.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.Hostname != "desk" || info.Paired() || !info.Sunshine() || info.Busy() || info.Codecs&CodecHEVC == 0 {
		t.Errorf("info = %+v", info)
	}
	if _, err := h.Apps(t.Context()); !errors.Is(err, ErrNotPaired) {
		t.Errorf("apps before pairing: %v", err)
	}
	if err := h.Pair(t.Context(), "4821", "laptop"); err != nil {
		t.Fatal(err)
	}
	if h.Cert == nil {
		t.Fatal("the host's certificate was not kept")
	}
	if info, err := h.Info(t.Context()); err != nil || !info.Paired() {
		t.Errorf("after pairing: %+v, %v", info, err)
	}
	apps, err := h.Apps(t.Context())
	if err != nil || len(apps) != 2 || apps[0].Title != "Desktop" || apps[0].ID != 881448767 {
		t.Errorf("apps = %+v, %v", apps, err)
	}
	l := Launch{AppID: 881448767, Width: 1920, Height: 1080, FPS: 60, KeyID: 0x80000001}
	l.Key[0] = 0xAB
	session, err := h.Start(t.Context(), l)
	if err != nil || session != "rtsp://127.0.0.1:48010" {
		t.Fatalf("start = %q, %v", session, err)
	}
	q := f.launched
	if q.Get("mode") != "1920x1080x60" || q.Get("rikey")[:2] != "ab" || len(q.Get("rikey")) != 32 ||
		q.Get("rikeyid") != strconv.Itoa(int(int32(-2147483647))) || q.Get("surroundAudioInfo") != "196610" {
		t.Errorf("launched with %v", q)
	}
}

func TestPairWithTheWrongPIN(t *testing.T) {
	f := newFakeHost(t, "1111")
	h := newClient(t, f.addr())
	if err := h.Pair(t.Context(), "2222", "laptop"); !errors.Is(err, ErrWrongPIN) {
		t.Fatalf("err = %v", err)
	}
	if h.Cert != nil {
		t.Error("a certificate kept from a failed pairing")
	}
}

func TestAHostWithAnotherCertificateIsNotTrusted(t *testing.T) {
	f := newFakeHost(t, "4821")
	h := newClient(t, f.addr())
	if err := h.Pair(t.Context(), "4821", "laptop"); err != nil {
		t.Fatal(err)
	}
	// Another host, at the same address as far as the client knows.
	other, _ := NewIdentity()
	h.Cert = other.CertPEM
	if _, err := h.Apps(t.Context()); err == nil {
		t.Error("trusted a certificate other than the one paired")
	}
}

func TestIdentityIsKept(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadIdentity(dir)
	if err != nil || a.UniqueID != b.UniqueID || !bytes.Equal(a.CertPEM, b.CertPEM) {
		t.Fatalf("a new identity each time: %v", err)
	}
	if len(a.UniqueID) != 16 {
		t.Errorf("unique ID %q", a.UniqueID)
	}
	if pin := NewPIN(); len(pin) != 4 {
		t.Errorf("PIN %q", pin)
	}
}
