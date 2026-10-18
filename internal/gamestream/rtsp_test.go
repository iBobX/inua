package gamestream

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// rtspHost answers RTSP as Sunshine does, one request a connection, and
// records what it was asked.
type rtspHost struct {
	ln       net.Listener
	mu       sync.Mutex
	requests []string // "METHOD target"
	sessions []string // the Session header of each request
	sdp      string
	describe string
	ports    [3]int // audio, video, control
}

func newRTSPHost(t *testing.T, describe string) *rtspHost {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &rtspHost{ln: ln, describe: describe, ports: [3]int{48000, 47998, 47999}}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go h.serve(c)
		}
	}()
	return h
}

func (h *rtspHost) serve(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	f := strings.Fields(line)
	hdr := map[string]string{}
	for {
		l, _ := br.ReadString('\n')
		l = strings.TrimSpace(l)
		if l == "" {
			break
		}
		k, v, _ := strings.Cut(l, ":")
		hdr[strings.ToLower(k)] = strings.TrimSpace(v)
	}
	var body []byte
	if n, err := strconv.Atoi(hdr["content-length"]); err == nil {
		body = make([]byte, n)
		io.ReadFull(br, body)
	}
	h.mu.Lock()
	h.requests = append(h.requests, f[0]+" "+f[1])
	h.sessions = append(h.sessions, hdr["session"])
	if f[0] == "ANNOUNCE" {
		h.sdp = string(body)
	}
	h.mu.Unlock()
	reply := "RTSP/1.0 200 OK\r\nCSeq: " + hdr["cseq"] + "\r\n"
	var payload string
	switch f[0] {
	case "DESCRIBE":
		payload = h.describe
	case "SETUP":
		reply += "Session: DEADBEEFCAFE;timeout = 90\r\n"
		switch {
		case strings.Contains(f[1], "audio"):
			reply += fmt.Sprintf("Transport: server_port=%d\r\nX-SS-Ping-Payload: 0123456789abcdef\r\n", h.ports[0])
		case strings.Contains(f[1], "video"):
			reply += fmt.Sprintf("Transport: server_port=%d\r\nX-SS-Ping-Payload: 0123456789abcdef\r\n", h.ports[1])
		default:
			reply += fmt.Sprintf("Transport: server_port=%d\r\nX-SS-Connect-Data: 3735928559\r\n", h.ports[2])
		}
	}
	if payload != "" {
		reply += fmt.Sprintf("Content-length: %d\r\n", len(payload))
	}
	c.Write([]byte(reply + "\r\n" + payload))
}

const sunshineDescribe = "a=x-ss-general.featureFlags:3\na=x-ss-general.encryptionSupported:7\n" +
	"a=x-ss-general.encryptionRequested:1\nsprop-parameter-sets=AAAAAU\na=rtpmap:98 AV1/90000\n"

func TestHandshakeSetsTheSessionUp(t *testing.T) {
	h := newRTSPHost(t, sunshineDescribe)
	_, port, _ := net.SplitHostPort(h.ln.Addr().String())
	cfg := Config{Width: 2560, Height: 1440, FPS: 60, BitrateKbps: 30000, Codec: HEVC, PacketSize: 1392, AudioPacketMs: 5}
	hs, err := handshake(t.Context(), "rtsp://192.168.9.9:"+port, "127.0.0.1", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hs.AudioPort != 48000 || hs.VideoPort != 47998 || hs.ControlPort != 47999 || hs.ConnectData != 3735928559 ||
		hs.PingPayload != "0123456789abcdef" || !hs.HEVC || !hs.AV1 || hs.EncryptionSupported != 7 {
		t.Errorf("handshake = %+v", hs)
	}
	want := []string{"OPTIONS rtsp://127.0.0.1:" + port, "DESCRIBE rtsp://127.0.0.1:" + port, "SETUP streamid=audio/0/0",
		"SETUP streamid=video/0/0", "SETUP streamid=control/13/0", "ANNOUNCE streamid=control/13/0",
		"PLAY streamid=video", "PLAY streamid=audio"}
	if strings.Join(h.requests, "|") != strings.Join(want, "|") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(h.requests, "\n"), strings.Join(want, "\n"))
	}
	for i, s := range h.sessions[3:] {
		if s != "DEADBEEFCAFE" {
			t.Errorf("request %d had session %q", i+3, s)
		}
	}
	for _, a := range []string{
		"a=x-nv-video[0].clientViewportWd:2560 ", "a=x-nv-video[0].clientViewportHt:1440 ", "a=x-nv-video[0].maxFPS:60 ",
		"a=x-nv-vqos[0].bitStreamFormat:1 ", "a=x-ss-general.encryptionEnabled:1 ", "a=x-ml-general.featureFlags:2 ",
		"a=x-nv-general.useReliableUdp:13 ", "a=x-nv-vqos[0].bw.maximumBitrateKbps:30000 ", "a=x-nv-audio.surround.numChannels:2 ",
		"a=x-nv-aqos.packetDuration:5 ", "a=x-nv-video[0].packetSize:1392 ", "m=video 47998  ",
	} {
		if !strings.Contains(h.sdp, a+"\r\n") {
			t.Errorf("SDP lacks %q", a)
		}
	}
	// Every attribute Sunshine reads without a default.
	for _, name := range []string{"x-nv-audio.surround.AudioQuality", "x-nv-audio.surround.channelMask", "x-nv-video[0].videoEncoderSlicesPerFrame",
		"x-nv-video[0].maxNumReferenceFrames", "x-ml-video.configuredBitrateKbps", "x-nv-general.featureFlags"} {
		if !strings.Contains(h.sdp, "a="+name+":") {
			t.Errorf("SDP lacks %s", name)
		}
	}
}

func TestHandshakeRefusesWhatItCannotDo(t *testing.T) {
	h := newRTSPHost(t, "a=x-ss-general.encryptionSupported:7\na=x-ss-general.encryptionRequested:7\n")
	_, port, _ := net.SplitHostPort(h.ln.Addr().String())
	if _, err := handshake(t.Context(), "rtsp://127.0.0.1:"+port, "127.0.0.1", Config{}); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("mandatory encryption: %v", err)
	}
	if _, err := handshake(t.Context(), "rtspenc://127.0.0.1:"+port, "127.0.0.1", Config{}); err == nil {
		t.Error("an encrypted RTSP session was taken")
	}
}
