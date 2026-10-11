package polkit

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
)

// Where polkit's helper is: started by systemd for each connection to the
// socket (polkit 126 and later), else run as a setuid program.
var (
	HelperSocket = "/run/polkit/agent-helper.socket"
	HelperPath   = "/usr/lib/polkit-1/polkit-agent-helper-1"
)

// EventKind is what a line from the helper says.
type EventKind int

const (
	Prompt   EventKind = iota // asks for something not shown as typed: a password
	Question                  // asks for something shown as typed
	Info                      // says something: "Place your finger on the reader"
	Problem                   // says something went wrong
	Done                      // the end: OK says whether it succeeded
)

// Event is one thing the helper said.
type Event struct {
	Kind EventKind
	Text string
	OK   bool // for Done
}

// Conversation is one attempt to authenticate, with the helper's PAM
// conversation in between: prompts to answer and messages to show, then
// whether it worked. On success the helper has already told polkitd. A
// failed attempt ends it; trying again takes a new Conversation.
type Conversation struct {
	rw   io.ReadWriteCloser
	r    *bufio.Reader
	wait func() // reaps a spawned helper
}

// Start begins authenticating as user for the request named by cookie.
func Start(user, cookie string) (*Conversation, error) {
	if strings.ContainsAny(user+cookie, "\n\x00") {
		return nil, errors.New("polkit: bad user or cookie")
	}
	if conn, err := net.Dial("unix", HelperSocket); err == nil {
		c := &Conversation{rw: conn, r: bufio.NewReader(conn)}
		// The socket-activated helper reads who, then which request.
		if _, err := io.WriteString(conn, user+"\n"+cookie+"\n"); err != nil {
			conn.Close()
			return nil, fmt.Errorf("polkit: %w", err)
		}
		return c, nil
	}
	return spawn(user, cookie)
}

// spawn runs the setuid helper, for a polkit without the socket.
func spawn(user, cookie string) (*Conversation, error) {
	cmd := exec.Command(HelperPath, user)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("polkit: no helper: %w", err)
	}
	if _, err := io.WriteString(in, cookie+"\n"); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("polkit: %w", err)
	}
	rw := pipes{out, in, cmd}
	return &Conversation{rw: rw, r: bufio.NewReader(out), wait: func() { cmd.Wait() }}, nil
}

type pipes struct {
	io.ReadCloser
	w   io.WriteCloser
	cmd *exec.Cmd
}

func (p pipes) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipes) Close() error {
	p.w.Close()
	p.ReadCloser.Close()
	p.cmd.Process.Kill()
	return nil
}

// Next waits for the helper's next event. An error, the helper gone
// without a word, counts as a failure.
func (c *Conversation) Next() (Event, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = errors.New("polkit: the helper stopped")
		}
		return Event{Kind: Done}, err
	}
	return parse(strings.TrimSuffix(line, "\n")), nil
}

func parse(line string) Event {
	for prefix, kind := range map[string]EventKind{
		"PAM_PROMPT_ECHO_OFF": Prompt,
		"PAM_PROMPT_ECHO_ON":  Question,
		"PAM_ERROR_MSG":       Problem,
		"PAM_TEXT_INFO":       Info,
	} {
		if rest, ok := strings.CutPrefix(line, prefix); ok && (rest == "" || rest[0] == ' ') {
			return Event{Kind: kind, Text: unescape(strings.TrimPrefix(rest, " "))}
		}
	}
	switch line {
	case "SUCCESS":
		return Event{Kind: Done, OK: true}
	case "FAILURE":
		return Event{Kind: Done}
	}
	return Event{Kind: Info, Text: line} // unknown; shown rather than lost
}

// Respond answers the last prompt.
func (c *Conversation) Respond(answer string) error {
	answer = strings.Map(func(r rune) rune {
		if r == '\n' || r == 0 {
			return -1
		}
		return r
	}, answer)
	_, err := io.WriteString(c.rw, answer+"\n")
	return err
}

// Close ends the conversation; one in progress fails.
func (c *Conversation) Close() error {
	err := c.rw.Close()
	if c.wait != nil {
		c.wait()
	}
	return err
}

// unescape undoes g_strescape, which the helper sends messages through:
// \n, \t, \\, \" and the like, and bytes outside printable ASCII (UTF-8
// among them) as three octal digits.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch c := s[i]; c {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v, n := 0, 0
			for ; n < 3 && i+n < len(s) && s[i+n] >= '0' && s[i+n] <= '7'; n++ {
				v = v*8 + int(s[i+n]-'0')
			}
			b.WriteByte(byte(v))
			i += n - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
