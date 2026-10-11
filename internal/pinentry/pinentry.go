// Package pinentry speaks gpg-agent's pinentry protocol (Assuan): the
// agent describes what it needs (SETDESC, SETPROMPT, SETERROR …) and asks
// for it (GETPIN, CONFIRM, MESSAGE); something else asks the user. inua's
// pinentry has the shell ask, in its password dialog.
package pinentry

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Mode is what the agent asks for.
type Mode string

const (
	GetPin  Mode = "getpin"  // a passphrase or PIN
	Confirm Mode = "confirm" // yes or no
	Message Mode = "message" // only to be read
)

// Request is one question, with what the agent set before asking it.
type Request struct {
	Mode    Mode   `json:"mode"`
	Title   string `json:"title,omitempty"`
	Desc    string `json:"desc,omitempty"`   // what for: "Please enter the passphrase to unlock …"
	Prompt  string `json:"prompt,omitempty"` // beside the field: "Passphrase:"
	Error   string `json:"error,omitempty"`  // why it asks again: "Bad Passphrase"
	OK      string `json:"ok,omitempty"`     // the buttons' labels, mnemonics stripped
	Cancel  string `json:"cancel,omitempty"`
	NotOK   string `json:"notok,omitempty"`  // a third button for CONFIRM
	Repeat  string `json:"repeat,omitempty"` // ask twice (a new passphrase): the second field's prompt
	NoMatch string `json:"nomatch,omitempty"`
	Timeout int    `json:"timeout,omitempty"` // seconds; 0 for none
}

// Answer is how the user answered.
type Answer struct {
	OK  bool   // OK (or the passphrase given); false for Cancel or Not OK
	Pin string // for GetPin
	// NotOK is the third button for Confirm, telling it from Cancel.
	NotOK bool
}

// Asker asks the user; an error means they could not be asked at all.
type Asker func(Request) (Answer, error)

// gpg-error codes with the pinentry source (5 << 24).
const (
	errCanceled     = 5<<24 | 99
	errNotConfirmed = 5<<24 | 114
	errUnknownCmd   = 5<<24 | 275
	errGeneral      = 5<<24 | 1
)

// Serve answers the agent on r and w until it says BYE or goes away.
func Serve(r io.Reader, w io.Writer, ask Asker) error {
	bw := bufio.NewWriter(w)
	say := func(format string, a ...any) error {
		fmt.Fprintf(bw, format+"\n", a...)
		return bw.Flush()
	}
	if err := say("OK Pleased to meet you, process %d", os.Getpid()); err != nil {
		return err
	}
	var st Request
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		cmd, arg, _ := strings.Cut(line, " ")
		arg = unescape(arg)
		cmd = strings.ToUpper(cmd)
		var err error
		switch cmd {
		case "BYE":
			return say("OK closing connection")
		case "NOP", "OPTION", "SETKEYINFO", "SETQUALITYBAR", "SETQUALITYBAR_TT", "SETGENPIN", "SETGENPIN_TT", "CLEARPASSPHRASE":
			err = say("OK")
		case "RESET":
			st = Request{}
			err = say("OK")
		case "SETTITLE":
			st.Title, err = arg, say("OK")
		case "SETDESC":
			st.Desc, err = arg, say("OK")
		case "SETPROMPT":
			st.Prompt, err = arg, say("OK")
		case "SETERROR":
			st.Error, err = arg, say("OK")
		case "SETOK":
			st.OK, err = label(arg), say("OK")
		case "SETCANCEL":
			st.Cancel, err = label(arg), say("OK")
		case "SETNOTOK":
			st.NotOK, err = label(arg), say("OK")
		case "SETREPEAT":
			st.Repeat, err = arg, say("OK")
			if st.Repeat == "" {
				st.Repeat = "Repeat:"
			}
		case "SETREPEATERROR":
			st.NoMatch, err = arg, say("OK")
		case "SETTIMEOUT":
			fmt.Sscan(arg, &st.Timeout)
			err = say("OK")
		case "GETINFO":
			switch arg {
			case "flavor":
				err = data(say, "inua")
			case "version":
				err = data(say, "1.0")
			case "pid":
				err = data(say, fmt.Sprint(os.Getpid()))
			case "ttyinfo":
				err = data(say, "- - - - 0/0 -")
			default:
				err = say("ERR %d Unknown command <Pinentry>", errUnknownCmd)
			}
		case "GETPIN":
			q := st
			q.Mode = GetPin
			err = answer(say, ask, q)
			st.Error = "" // shown once
		case "CONFIRM":
			q := st
			q.Mode = Confirm
			if arg == "--one-button" {
				q.Mode = Message
			}
			err = answer(say, ask, q)
			st.Error = ""
		case "MESSAGE":
			q := st
			q.Mode = Message
			err = answer(say, ask, q)
		default:
			err = say("ERR %d Unknown command <Pinentry>", errUnknownCmd)
		}
		if err != nil {
			return err
		}
	}
	return sc.Err()
}

// answer asks the user and tells the agent.
func answer(say func(string, ...any) error, ask Asker, q Request) error {
	a, err := ask(q)
	switch {
	case err != nil:
		return say("ERR %d %s <Pinentry>", errGeneral, oneLine(err.Error()))
	case q.Mode == GetPin && a.OK:
		if a.Pin != "" {
			if err := say("D %s", escape(a.Pin)); err != nil {
				return err
			}
		}
		return say("OK")
	case a.OK:
		return say("OK")
	case q.Mode == Confirm && a.NotOK:
		return say("ERR %d Not confirmed <Pinentry>", errNotConfirmed)
	}
	return say("ERR %d Operation cancelled <Pinentry>", errCanceled)
}

func data(say func(string, ...any) error, s string) error {
	if err := say("D %s", escape(s)); err != nil {
		return err
	}
	return say("OK")
}

// label drops a button label's mnemonic marks: "_OK" is "OK", "__" is "_".
func label(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if i+1 < len(s) && s[i+1] == '_' {
				b.WriteByte('_')
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unescape undoes Assuan's %XX escapes.
func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// escape writes %, CR and LF as Assuan's %XX for a data line.
func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '%', '\r', '\n':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func oneLine(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ") }
