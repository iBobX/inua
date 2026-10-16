// Package answer asks the user's AI short questions from another program,
// as the games do for an AI player: `inua-assistant --answer` answers them
// on its standard input and output, a JSON object a line, with a profile
// of Settings → AI and no tools. It first says which profiles can answer,
// the one that answers by default first, or why none can. A question is
// free (Ask), or a choice from a list (Choose): Mister Morph's decision
// route, else asked as a question with the options numbered. It does not
// import Mister Morph, so the games can use it.
package answer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Profile is a profile that can answer, and its model.
type Profile struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

// hello is the first line: the profiles that can answer, or why none can.
type hello struct {
	Profiles []Profile `json:"profiles,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// request is a question: Prompt, or with Options a choice, Instructions
// saying what to choose and State what to judge.
type request struct {
	ID           int      `json:"id"`
	Profile      string   `json:"profile,omitempty"` // none: the default
	Prompt       string   `json:"prompt,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	State        string   `json:"state,omitempty"`
	Options      []string `json:"options,omitempty"`
}

type reply struct {
	ID     int    `json:"id"`
	Answer string `json:"answer,omitempty"`
	Choice *int   `json:"choice,omitempty"` // the option picked, from 0
	Error  string `json:"error,omitempty"`
}

// Answerer answers, for Serve.
type Answerer interface {
	Ask(ctx context.Context, profile, prompt string) (string, error)
	// Choose picks one of options, its index; ErrCannotChoose when the
	// profile cannot choose so, for Serve to ask instead.
	Choose(ctx context.Context, profile, instructions, state string, options []string) (int, error)
}

// ErrCannotChoose is a profile that cannot choose from a list, only answer.
var ErrCannotChoose = errors.New("answer: this profile cannot choose from a list")

// ChoicePrompt is a choice asked as a question: the instructions, the
// state, the options numbered.
func ChoicePrompt(instructions, state string, options []string) string {
	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\n\n")
	b.WriteString(state)
	b.WriteString("\n\nThe options:\n")
	for i, o := range options {
		fmt.Fprintf(&b, "%d. %s\n", i+1, o)
	}
	b.WriteString("\nAnswer with the number of your choice and nothing else.")
	return b.String()
}

var number = regexp.MustCompile(`\d+`)

// ParseChoice is the option an answer names, from 0: the first number in
// it, if it is one of the n; -1 for none.
func ParseChoice(answer string, n int) int {
	s := number.FindString(answer)
	if s == "" {
		return -1
	}
	i, err := strconv.Atoi(s)
	if err != nil || i < 1 || i > n {
		return -1
	}
	return i - 1
}

// choose answers a choice: with a.Choose, else as a question.
func choose(ctx context.Context, a Answerer, q request) (int, error) {
	i, err := a.Choose(ctx, q.Profile, q.Instructions, q.State, q.Options)
	if !errors.Is(err, ErrCannotChoose) {
		return i, err
	}
	text, err := a.Ask(ctx, q.Profile, ChoicePrompt(q.Instructions, q.State, q.Options))
	if err != nil {
		return -1, err
	}
	if i = ParseChoice(text, len(q.Options)); i < 0 {
		return -1, fmt.Errorf("answer: %q names none of the %d options", text, len(q.Options))
	}
	return i, nil
}

// Serve answers the questions read from r on w, after the hello: the
// profiles that can answer, or failed says why none can. Each question is
// answered in turn, by the profile it names.
func Serve(ctx context.Context, r io.Reader, w io.Writer, profiles []Profile, failed error, a Answerer) error {
	enc := json.NewEncoder(w)
	if failed != nil {
		return enc.Encode(hello{Error: failed.Error()})
	}
	if err := enc.Encode(hello{Profiles: profiles}); err != nil {
		return err
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		var q request
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			continue
		}
		rep := reply{ID: q.ID}
		var err error
		if len(q.Options) > 0 {
			var i int
			if i, err = choose(ctx, a, q); err == nil {
				rep.Choice = &i
			}
		} else {
			rep.Answer, err = a.Ask(ctx, q.Profile, q.Prompt)
		}
		if err != nil {
			rep.Error = err.Error()
		}
		if err := enc.Encode(rep); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return sc.Err()
}

// Client is inua-assistant --answer, started for asking.
type Client struct {
	Profiles []Profile // the default first

	cmd     *exec.Cmd
	in      io.WriteCloser
	mu      sync.Mutex // one question at a time
	id      int
	replies chan reply
}

// Program is inua-assistant: beside this program, else on the PATH.
func Program() (string, error) {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "inua-assistant")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return exec.LookPath("inua-assistant")
}

// Start starts the program with args (inua-assistant --answer) and waits
// for its hello: the error is why it cannot answer, in its words.
func Start(ctx context.Context, program string, args ...string) (*Client, error) {
	cmd := exec.Command(program, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, in: in, replies: make(chan reply, 1)}
	hellos := make(chan hello, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 4<<20)
		first := true
		for sc.Scan() {
			if first {
				first = false
				var h hello
				if json.Unmarshal(sc.Bytes(), &h) != nil {
					h.Error = "inua-assistant: " + sc.Text()
				}
				hellos <- h
				continue
			}
			var a reply
			if json.Unmarshal(sc.Bytes(), &a) == nil {
				c.replies <- a
			}
		}
		if first {
			hellos <- hello{Error: "inua-assistant ended without answering"}
		}
		close(c.replies)
		_ = cmd.Wait()
	}()
	select {
	case h := <-hellos:
		if h.Error != "" {
			c.Close()
			return nil, errors.New(h.Error)
		}
		if len(h.Profiles) == 0 {
			c.Close()
			return nil, errors.New("inua-assistant has no profile to answer with")
		}
		c.Profiles = h.Profiles
		return c, nil
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	}
}

// ErrClosed is a question to a program that has ended.
var ErrClosed = errors.New("answer: inua-assistant has ended")

// Ask asks profile (none: the default) a question and waits for its
// answer, or for ctx to end.
func (c *Client) Ask(ctx context.Context, profile, prompt string) (string, error) {
	a, err := c.do(ctx, request{Profile: profile, Prompt: prompt})
	return a.Answer, err
}

// Choose has profile (none: the default) pick one of options, as
// instructions say, judging state; it returns the index picked.
func (c *Client) Choose(ctx context.Context, profile, instructions, state string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, errors.New("answer: no options")
	}
	a, err := c.do(ctx, request{Profile: profile, Instructions: instructions, State: state, Options: options})
	if err != nil {
		return -1, err
	}
	if a.Choice == nil || *a.Choice < 0 || *a.Choice >= len(options) {
		return -1, errors.New("answer: no option chosen")
	}
	return *a.Choice, nil
}

// do sends q and waits for its reply, or for ctx to end.
func (c *Client) do(ctx context.Context, q request) (reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++
	q.ID = c.id
	b, _ := json.Marshal(q)
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		return reply{}, ErrClosed
	}
	for {
		select {
		case a, ok := <-c.replies:
			if !ok {
				return reply{}, ErrClosed
			}
			if a.ID != c.id {
				continue // the answer to a question given up on
			}
			if a.Error != "" {
				return reply{}, errors.New(a.Error)
			}
			return a, nil
		case <-ctx.Done():
			return reply{}, ctx.Err()
		}
	}
}

// Close ends the program.
func (c *Client) Close() {
	_ = c.in.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}
