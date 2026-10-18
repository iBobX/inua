package gamestream

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultPort is a host's HTTP port when none is given; HTTPS is the one
// its server info names (47984 by default).
const DefaultPort = 47989

// The codecs a host can stream (ServerCodecModeSupport).
const (
	CodecH264       = 0x00000001
	CodecHEVC       = 0x00000100
	CodecHEVCMain10 = 0x00000200
	CodecAV1        = 0x00010000
	CodecAV1Main10  = 0x00020000
)

// Host is a GameStream host at an address, and this client's identity to
// it. Cert is the host's certificate (PEM) once paired: it is pinned, and
// HTTPS to the host is trusted by it alone.
type Host struct {
	Addr string // host, or host:port of its HTTP port
	ID   *Identity
	Cert []byte

	mu        sync.Mutex
	httpsPort int
	pairing   []byte // the certificate the host gave while pairing, trusted until it ends
}

// ServerInfo is what a host says of itself.
type ServerInfo struct {
	Hostname    string `xml:"hostname"`
	AppVersion  string `xml:"appversion"`
	GfeVersion  string `xml:"GfeVersion"`
	UniqueID    string `xml:"uniqueid"`
	MAC         string `xml:"mac"`
	LocalIP     string `xml:"LocalIP"`
	HTTPSPort   int    `xml:"HttpsPort"`
	CurrentGame int    `xml:"currentgame"`
	State       string `xml:"state"`
	PairStatus  int    `xml:"PairStatus"`
	Codecs      uint32 `xml:"ServerCodecModeSupport"`
}

// Paired says whether the host knows this client.
func (s ServerInfo) Paired() bool { return s.PairStatus == 1 }

// Busy says whether a session is running on the host.
func (s ServerInfo) Busy() bool { return strings.HasSuffix(s.State, "_SERVER_BUSY") }

// Sunshine says whether the host is Sunshine rather than Nvidia's
// GameStream: its version's fourth part is negative.
func (s ServerInfo) Sunshine() bool {
	parts := strings.Split(s.AppVersion, ".")
	return len(parts) == 4 && strings.HasPrefix(parts[3], "-")
}

// App is one of the host's apps (Sunshine's "Desktop" among them).
type App struct {
	Title string `xml:"AppTitle"`
	ID    int    `xml:"ID"`
}

// StatusError is a host's answer with a status other than 200.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("gamestream: the host answered %d: %s", e.Code, e.Message)
}

// ErrNotPaired is a host that does not know this client.
var ErrNotPaired = errors.New("gamestream: not paired with the host")

// host is the address's host name, and port its HTTP port.
func (h *Host) hostPort() (string, int) {
	host, port, err := net.SplitHostPort(h.Addr)
	if err != nil {
		return strings.Trim(h.Addr, "[]"), DefaultPort
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		p = DefaultPort
	}
	return host, p
}

// Hostname is the host's name or address, without a port.
func (h *Host) Hostname() string {
	host, _ := h.hostPort()
	return host
}

func (h *Host) baseURL(secure bool) string {
	host, port := h.hostPort()
	scheme := "http"
	if secure {
		scheme = "https"
		h.mu.Lock()
		port = h.httpsPort
		h.mu.Unlock()
		if port == 0 {
			port = DefaultPort - 5 // 47984
		}
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// client is HTTP, or HTTPS with this client's certificate, trusting only
// the host's pinned certificate (or, while pairing, the one it gave).
func (h *Host) client(secure bool, timeout time.Duration) *http.Client {
	tr := &http.Transport{DisableKeepAlives: true}
	if secure {
		tr.TLSClientConfig = &tls.Config{
			Certificates:       []tls.Certificate{h.ID.tlsCertificate()},
			InsecureSkipVerify: true, // a self-signed certificate, checked below against the pinned one
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				h.mu.Lock()
				pinned := h.Cert
				if h.pairing != nil {
					pinned = h.pairing
				}
				h.mu.Unlock()
				b, _ := pem.Decode(pinned)
				if b == nil || len(raw) == 0 || !bytes.Equal(raw[0], b.Bytes) {
					return errors.New("gamestream: the host's certificate is not the one paired with")
				}
				return nil
			},
		}
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// root is a response's outer element, with its status.
type root struct {
	Code    int    `xml:"status_code,attr"`
	Message string `xml:"status_message,attr"`
}

// get asks the host for path, with this client's IDs and args, and decodes
// the answer into v.
func (h *Host) get(ctx context.Context, secure bool, timeout time.Duration, path string, args url.Values, v any) error {
	if secure {
		h.mu.Lock()
		trusted, known := h.Cert != nil || h.pairing != nil, h.httpsPort != 0
		h.mu.Unlock()
		if !trusted {
			return ErrNotPaired
		}
		if !known { // the HTTPS port is the one the host names
			if _, err := h.plainInfo(ctx); err != nil {
				return err
			}
		}
	}
	if args == nil {
		args = url.Values{}
	}
	args.Set("uniqueid", h.ID.UniqueID)
	var uuid [16]byte
	rand.Read(uuid[:])
	args.Set("uuid", hex.EncodeToString(uuid[:]))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL(secure)+"/"+path+"?"+args.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := h.client(secure, timeout).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrNotPaired
	}
	var r root
	if err := xml.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("gamestream: %s: %w", path, err)
	}
	if r.Code == 401 {
		return ErrNotPaired
	}
	if r.Code != 200 {
		return &StatusError{Code: r.Code, Message: r.Message}
	}
	if v != nil {
		return xml.Unmarshal(body, v)
	}
	return nil
}

const requestTimeout = 10 * time.Second

// Info asks the host what it is: over HTTPS when paired (only then does it
// say so), else over HTTP.
// A host that refuses the pinned certificate answers as not paired.
func (h *Host) Info(ctx context.Context) (ServerInfo, error) {
	plain, err := h.plainInfo(ctx)
	if err != nil || h.Cert == nil {
		return plain, err
	}
	var info ServerInfo
	if err := h.get(ctx, true, requestTimeout, "serverinfo", nil, &info); err != nil {
		return plain, nil
	}
	return info, nil
}

func (h *Host) plainInfo(ctx context.Context) (ServerInfo, error) {
	var info ServerInfo
	if err := h.get(ctx, false, requestTimeout, "serverinfo", nil, &info); err != nil {
		return info, err
	}
	h.mu.Lock()
	if info.HTTPSPort > 0 {
		h.httpsPort = info.HTTPSPort
	}
	h.mu.Unlock()
	return info, nil
}

// Apps lists the host's apps.
func (h *Host) Apps(ctx context.Context) ([]App, error) {
	var list struct {
		Apps []App `xml:"App"`
	}
	if err := h.get(ctx, true, requestTimeout, "applist", nil, &list); err != nil {
		return nil, err
	}
	return list.Apps, nil
}

// Launch is what a session is started with.
type Launch struct {
	AppID              int
	Width, Height, FPS int
	Key                [16]byte // the session's AES key (rikey)
	KeyID              uint32   // its IV's first four bytes (rikeyid)
	HostAudio          bool     // play the sound on the host too
	Resume             bool     // join the session already running
}

// Start launches the app, or resumes the session running, and returns the
// session's RTSP URL.
func (h *Host) Start(ctx context.Context, l Launch) (string, error) {
	args := url.Values{}
	args.Set("appid", strconv.Itoa(l.AppID))
	args.Set("mode", fmt.Sprintf("%dx%dx%d", l.Width, l.Height, l.FPS))
	args.Set("additionalStates", "1")
	args.Set("sops", "0")
	args.Set("rikey", hex.EncodeToString(l.Key[:]))
	args.Set("rikeyid", strconv.FormatInt(int64(int32(l.KeyID)), 10))
	local := "0"
	if l.HostAudio {
		local = "1"
	}
	args.Set("localAudioPlayMode", local)
	args.Set("surroundAudioInfo", strconv.Itoa(0x3<<16|2)) // stereo
	args.Set("remoteControllersBitmap", "0")
	args.Set("gcmap", "0")
	args.Set("gcpersist", "0")
	verb := "launch"
	if l.Resume {
		verb = "resume"
	}
	var r struct {
		Session string `xml:"sessionUrl0"`
	}
	if err := h.get(ctx, true, 2*time.Minute, verb, args, &r); err != nil {
		return "", err
	}
	if r.Session == "" {
		host, _ := h.hostPort()
		r.Session = "rtsp://" + net.JoinHostPort(host, "48010")
	}
	return r.Session, nil
}

// Quit ends the session running on the host.
func (h *Host) Quit(ctx context.Context) error {
	return h.get(ctx, true, requestTimeout, "cancel", nil, nil)
}
