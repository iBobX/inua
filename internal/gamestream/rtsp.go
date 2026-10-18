package gamestream

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Codec is the video's format.
type Codec int

const (
	H264 Codec = iota
	HEVC
	AV1
)

// Config is what a session streams.
type Config struct {
	Width, Height, FPS int
	BitrateKbps        int
	Codec              Codec
	PacketSize         int // video payload per packet; 1392 suits most networks
	AudioPacketMs      int // 5 or 10
}

// The encryption a host supports and asks for (x-ss-general.encryption*).
const (
	encControlV2 = 0x01
	encVideo     = 0x02
	encAudio     = 0x04
)

// Handshake is what RTSP sets a session up with: the streams' ports, the
// payload the video and audio pings carry, the control stream's connect
// data, and what the host offers.
type Handshake struct {
	AudioPort, VideoPort, ControlPort int
	PingPayload                       string
	ConnectData                       uint32
	EncryptionSupported               uint32
	EncryptionRequested               uint32
	HEVC, AV1                         bool
}

// rtspClient sends a session's RTSP requests, each on a connection of its
// own, as Sunshine expects.
type rtspClient struct {
	addr    string // host:port
	host    string
	target  string // rtsp://host:port
	seq     int
	session string
}

func newRTSP(sessionURL, host string) (*rtspClient, error) {
	u, err := url.Parse(sessionURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "rtsp":
	case "rtspenc":
		return nil, errors.New("gamestream: the host asks for encrypted RTSP, which this client does not speak")
	default:
		return nil, fmt.Errorf("gamestream: %q is not an RTSP session", sessionURL)
	}
	port := u.Port()
	if port == "" {
		port = "48010"
	}
	// The host's session URL may name an address only it knows by.
	addr := net.JoinHostPort(host, port)
	return &rtspClient{addr: addr, host: host, target: "rtsp://" + addr}, nil
}

type rtspResponse struct {
	status  int
	headers map[string]string
	body    []byte
}

func (r *rtspClient) do(ctx context.Context, method, target string, headers [][2]string, body []byte) (*rtspResponse, error) {
	r.seq++
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s RTSP/1.0\r\nCSeq: %d\r\nX-GS-ClientVersion: 14\r\nHost: %s\r\n", method, target, r.seq, r.host)
	for _, h := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", h[0], h[1])
	}
	b.WriteString("\r\n")
	b.Write(body)

	var conn net.Conn
	var err error
	d := net.Dialer{Timeout: 10 * time.Second}
	// The host may not listen yet just after a launch.
	for try := 0; try < 20; try++ {
		conn, err = d.DialContext(ctx, "tcp", r.addr)
		if err == nil || ctx.Err() != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := conn.Write(b.Bytes()); err != nil {
		return nil, err
	}
	return readRTSP(bufio.NewReader(conn))
}

func readRTSP(br *bufio.Reader) (*rtspResponse, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "RTSP/") {
		return nil, fmt.Errorf("gamestream: not an RTSP response: %q", strings.TrimSpace(line))
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, err
	}
	resp := &rtspResponse{status: status, headers: map[string]string{}}
	for {
		line, err := br.ReadString('\n')
		if err != nil && line == "" {
			return resp, nil
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			resp.headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	if n, err := strconv.Atoi(resp.headers["content-length"]); err == nil && n >= 0 && n < 1<<20 {
		resp.body = make([]byte, n)
		_, err := io.ReadFull(br, resp.body)
		return resp, err
	}
	resp.body, _ = io.ReadAll(io.LimitReader(br, 1<<20))
	return resp, nil
}

func (r *rtspClient) ok(ctx context.Context, method, target string, headers [][2]string, body []byte) (*rtspResponse, error) {
	resp, err := r.do(ctx, method, target, headers, body)
	if err != nil {
		return nil, fmt.Errorf("gamestream: RTSP %s: %w", method, err)
	}
	if resp.status != 200 {
		return nil, fmt.Errorf("gamestream: RTSP %s %s: the host answered %d", method, target, resp.status)
	}
	return resp, nil
}

// serverPort reads server_port=N from a SETUP response's Transport.
func serverPort(resp *rtspResponse) (int, error) {
	for _, part := range strings.Split(resp.headers["transport"], ";") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "server_port="); ok {
			if p, err := strconv.Atoi(strings.TrimSpace(strings.Split(v, "-")[0])); err == nil && p > 0 && p < 65536 {
				return p, nil
			}
		}
	}
	return 0, errors.New("gamestream: a SETUP without its server port")
}

const ifModified = "Thu, 01 Jan 1970 00:00:00 GMT"

// handshake sets the session up: what the host offers, the streams'
// ports, then the session's description, and the streams started.
func handshake(ctx context.Context, sessionURL, host string, cfg Config) (Handshake, error) {
	var hs Handshake
	r, err := newRTSP(sessionURL, host)
	if err != nil {
		return hs, err
	}
	if _, err := r.ok(ctx, "OPTIONS", r.target, nil, nil); err != nil {
		return hs, err
	}
	desc, err := r.ok(ctx, "DESCRIBE", r.target, [][2]string{{"Accept", "application/sdp"}, {"If-Modified-Since", ifModified}}, nil)
	if err != nil {
		return hs, err
	}
	for _, line := range strings.Split(string(desc.body), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "sprop-parameter-sets=AAAAAU"):
			hs.HEVC = true
		case strings.HasPrefix(line, "a=rtpmap:98 AV1/90000"):
			hs.AV1 = true
		}
		if v, ok := strings.CutPrefix(line, "a=x-ss-general.encryptionSupported:"); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
			hs.EncryptionSupported = uint32(n)
		}
		if v, ok := strings.CutPrefix(line, "a=x-ss-general.encryptionRequested:"); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
			hs.EncryptionRequested = uint32(n)
		}
	}
	if hs.EncryptionSupported&encControlV2 == 0 {
		return hs, errors.New("gamestream: the host does not encrypt its control stream as Sunshine does")
	}
	if hs.EncryptionRequested&(encVideo|encAudio) != 0 {
		return hs, errors.New("gamestream: the host requires its video and audio encrypted, which this client does not do (Sunshine: lan_encryption_mode = 0)")
	}

	setup := func(stream string) (*rtspResponse, int, error) {
		h := [][2]string{{"Transport", "unicast;X-GS-ClientPort=50000-50001"}, {"If-Modified-Since", ifModified}}
		if r.session != "" {
			h = append([][2]string{{"Session", r.session}}, h...)
		}
		resp, err := r.ok(ctx, "SETUP", stream, h, nil)
		if err != nil {
			return nil, 0, err
		}
		port, err := serverPort(resp)
		return resp, port, err
	}
	resp, port, err := setup("streamid=audio/0/0")
	if err != nil {
		return hs, err
	}
	hs.AudioPort = port
	r.session, _, _ = strings.Cut(resp.headers["session"], ";")
	if r.session == "" {
		return hs, errors.New("gamestream: the audio SETUP gave no session")
	}
	hs.PingPayload = resp.headers["x-ss-ping-payload"]
	if _, hs.VideoPort, err = setup("streamid=video/0/0"); err != nil {
		return hs, err
	}
	resp, hs.ControlPort, err = setup("streamid=control/13/0")
	if err != nil {
		return hs, err
	}
	if v, err := strconv.ParseUint(resp.headers["x-ss-connect-data"], 10, 32); err == nil {
		hs.ConnectData = uint32(v)
	}
	if hs.PingPayload == "" {
		return hs, errors.New("gamestream: the host gave no ping payload; it is older than this client supports")
	}

	sdp := sessionDescription(host, cfg, hs)
	if _, err := r.ok(ctx, "ANNOUNCE", "streamid=control/13/0", [][2]string{
		{"Session", r.session}, {"Content-type", "application/sdp"}, {"Content-length", strconv.Itoa(len(sdp))},
	}, sdp); err != nil {
		return hs, err
	}
	for _, stream := range []string{"streamid=video", "streamid=audio"} {
		if _, err := r.ok(ctx, "PLAY", stream, [][2]string{{"Session", r.session}}, nil); err != nil {
			return hs, err
		}
	}
	return hs, nil
}

// sessionDescription is the ANNOUNCE's SDP: what the client wants the host
// to stream, each attribute the host requires.
func sessionDescription(host string, cfg Config, hs Handshake) []byte {
	family := "IPv4"
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		family = "IPv6"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "v=0\r\no=android 0 14 IN %s %s\r\ns=NVIDIA Streaming Client\r\n", family, host)
	attr := func(name string, v any) { fmt.Fprintf(&b, "a=%s:%v \r\n", name, v) }
	attr("x-ml-general.featureFlags", 0x02)              // ML_FF_SESSION_ID_V1: the ping payload and connect data
	attr("x-ss-general.encryptionEnabled", encControlV2) // the control stream, input with it; not video or audio
	attr("x-ss-video[0].chromaSamplingType", 0)
	attr("x-ss-video[0].intraRefresh", 0)
	attr("x-nv-video[0].clientViewportWd", cfg.Width)
	attr("x-nv-video[0].clientViewportHt", cfg.Height)
	attr("x-nv-video[0].maxFPS", cfg.FPS)
	attr("x-nv-video[0].clientRefreshRateX100", cfg.FPS*100)
	attr("x-nv-video[0].packetSize", cfg.PacketSize)
	attr("x-nv-video[0].rateControlMode", 4)
	attr("x-nv-video[0].timeoutLengthMs", 7000)
	attr("x-nv-video[0].framesWithInvalidRefThreshold", 0)
	attr("x-nv-video[0].initialBitrateKbps", cfg.BitrateKbps)
	attr("x-nv-video[0].initialPeakBitrateKbps", cfg.BitrateKbps)
	attr("x-nv-vqos[0].bw.minimumBitrateKbps", cfg.BitrateKbps)
	attr("x-nv-vqos[0].bw.maximumBitrateKbps", cfg.BitrateKbps)
	attr("x-ml-video.configuredBitrateKbps", cfg.BitrateKbps)
	attr("x-nv-vqos[0].fec.enable", 1)
	attr("x-nv-vqos[0].videoQualityScoreUpdateTime", 5000)
	attr("x-nv-vqos[0].qosTrafficType", 5)
	attr("x-nv-aqos.qosTrafficType", 4)
	attr("x-nv-general.featureFlags", 0x07|0x80) // the base features, and remote input encrypted
	attr("x-nv-general.useReliableUdp", 13)      // the encrypted control protocol
	attr("x-nv-vqos[0].fec.minRequiredFecPackets", 2)
	attr("x-nv-vqos[0].bllFec.enable", 0)
	attr("x-nv-vqos[0].drc.enable", 0)
	attr("x-nv-general.enableRecoveryMode", 0)
	attr("x-nv-video[0].videoEncoderSlicesPerFrame", 1)
	attr("x-nv-clientSupportHevc", map[bool]int{true: 1, false: 0}[cfg.Codec == HEVC])
	attr("x-nv-vqos[0].bitStreamFormat", int(cfg.Codec))
	attr("x-nv-video[0].dynamicRangeMode", 0)
	attr("x-nv-video[0].maxNumReferenceFrames", 1) // a lost frame is mended by a new IDR frame
	attr("x-nv-audio.surround.numChannels", 2)
	attr("x-nv-audio.surround.channelMask", 0x3)
	attr("x-nv-audio.surround.enable", 0)
	attr("x-nv-audio.surround.AudioQuality", 1)
	attr("x-nv-aqos.packetDuration", cfg.AudioPacketMs)
	attr("x-nv-video[0].encoderCscMode", 1<<1|0) // Rec. 709, limited range
	fmt.Fprintf(&b, "t=0 0\r\nm=video %d  \r\n", hs.VideoPort)
	return b.Bytes()
}
