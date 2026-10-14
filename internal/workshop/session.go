package workshop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
)

// Host is the window a session draws in and asks things of. Its methods
// are called from the session's goroutines; the window brings them to its
// main thread.
type Host interface {
	Render(root Node)
	SetValues(values map[string]any)
	// State is every input's value by id, for an event.
	State() map[string]any
	Confirm(ctx context.Context, title, text, ok string, danger bool) (bool, error)
	Copy(text string, secret bool) error
	SetTitle(title string)
}

// Session is an app's program running for a window.
type Session struct {
	M    Manifest
	host Host
	// Trace, when set, sees the protocol's lines ("→" sent, "←" read)
	// and the app's stderr: `inua workshop dev`.
	Trace io.Writer

	cmd    *exec.Cmd
	conn   *Conn
	stderr tail
	exited chan struct{}
	err    error // why the program ended

	mu     sync.Mutex
	timers map[string]chan struct{}
	store  map[string]json.RawMessage
}

// NewSession prepares app m for host; Start runs it.
func NewSession(m Manifest, host Host) *Session {
	return &Session{M: m, host: host, exited: make(chan struct{}), timers: map[string]chan struct{}{}}
}

// Start runs the program and says hello: the window draws what it sends
// from then on.
func (s *Session) Start(language, theme string) error {
	if err := WriteLib(); err != nil {
		return fmt.Errorf("writing the helpers: %w", err)
	}
	argv := s.M.Run
	path := os.Getenv("PATH")
	cmd := exec.Command(argv[0], argv[1:]...)
	if dir := findRuntime(argv[0]); dir != "" {
		cmd.Path = filepath.Join(dir, argv[0])
		path = dir + string(os.PathListSeparator) + path // its own child processes find it too
	}
	if strings.HasPrefix(argv[0], "./") {
		cmd.Path = filepath.Join(s.M.Dir, argv[0])
	}
	cmd.Dir = s.M.Dir
	lib := LibDir()
	cmd.Env = append(os.Environ(),
		"PATH="+path,
		"INUA_WORKSHOP_LIB="+lib,
		"INUA_WORKSHOP_APP="+s.M.ID,
		"PYTHONPATH="+join(filepath.Join(lib, "python"), os.Getenv("PYTHONPATH")),
		"NODE_PATH="+join(filepath.Join(lib, "node"), os.Getenv("NODE_PATH")),
		"PYTHONUNBUFFERED=1", "PYTHONDONTWRITEBYTECODE=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	s.stderr.trace = s.Trace
	cmd.Stderr = &s.stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", strings.Join(argv, " "), err)
	}
	s.cmd = cmd
	s.conn = NewConn(stdout, stdin, s.handle)
	if s.Trace != nil {
		s.conn.Trace(func(l []byte) { fmt.Fprintf(s.Trace, "→ %s\n", l) }, func(l []byte) { fmt.Fprintf(s.Trace, "← %s\n", l) })
	}
	go func() {
		err := cmd.Wait()
		s.stopTimers()
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.exited)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var hello struct {
		Protocol int `json:"protocol"`
	}
	err = s.conn.Call(ctx, "initialize", map[string]any{
		"protocol": Protocol, "language": language, "theme": theme,
		"app": map[string]string{"id": s.M.ID, "name": s.M.Name}, "dir": s.M.Dir,
	}, &hello)
	switch {
	case err != nil:
		select {
		case <-s.exited:
			return errors.New(i18n.T("It ended before saying hello; what it wrote is below."))
		case <-time.After(300 * time.Millisecond):
		}
		s.kill()
		return fmt.Errorf("%s: %w", i18n.T("It did not answer"), err)
	case hello.Protocol != Protocol:
		s.kill()
		return errors.New(i18n.Tf("It speaks version %d of the workshop protocol; this inua speaks %d.", hello.Protocol, Protocol))
	}
	return nil
}

// findRuntime finds a program the session's PATH lacks where version
// managers and installers put it for the user (a desktop session's PATH
// seldom has nvm's node or ~/.bun/bin): the folder it is in, or "" when
// PATH has it or nothing does.
func findRuntime(name string) string {
	if strings.ContainsRune(name, '/') {
		return ""
	}
	if _, err := exec.LookPath(name); err == nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".deno", "bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".local", "share", "fnm", "aliases", "default", "bin"),
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, ".asdf", "shims"),
	}
	// nvm: the newest node installed.
	if vs, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "v*")); len(vs) > 0 {
		sort.Slice(vs, func(i, j int) bool { return newerVersion(filepath.Base(vs[i]), filepath.Base(vs[j])) })
		for _, v := range vs {
			dirs = append(dirs, filepath.Join(v, "bin"))
		}
	}
	for _, d := range dirs {
		if info, err := os.Stat(filepath.Join(d, name)); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return d
		}
	}
	return ""
}

// newerVersion says whether version a ("v24.14.1") is newer than b.
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func join(a, b string) string {
	if b == "" {
		return a
	}
	return a + string(os.PathListSeparator) + b
}

// Exited is closed when the program ended; Why says why.
func (s *Session) Exited() <-chan struct{} { return s.exited }

// Why is how the program ended, and the end of what it wrote to stderr.
func (s *Session) Why() (error, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err, s.stderr.String()
}

// Event tells the app what happened, with the inputs' values asked of the
// host: for the session's own events (timers). Not on the host's thread.
func (s *Session) Event(kind, id string, value any) {
	s.Send(kind, id, value, s.host.State())
}

// Send tells the app what the user did, with the inputs' values as the
// window has them.
func (s *Session) Send(kind, id string, value any, state map[string]any) {
	if s.conn == nil {
		return
	}
	ev := map[string]any{"type": kind, "id": id, "state": state}
	if value != nil {
		ev["value"] = value
	}
	s.conn.Notify("event", ev)
}

// Stop asks the app to end, and ends it if it does not within a few
// seconds.
func (s *Session) Stop() {
	if s.cmd == nil {
		return
	}
	s.stopTimers()
	select {
	case <-s.exited:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.conn.Call(ctx, "shutdown", nil, nil)
	select {
	case <-s.exited:
	case <-time.After(3 * time.Second):
		s.kill()
	}
}

func (s *Session) kill() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
}

// needs is the use each function requires; those not here need none.
var needs = map[string]string{
	"notify": "notify", "open": "open", "clipboard.copy": "clipboard",
	"clipboard.copySecret": "clipboard", "terminal.open": "terminal", "system.run": "run",
}

func bad(err error) error { return &Error{CodeBadParams, err.Error()} }

// handle answers the app.
func (s *Session) handle(method string, params json.RawMessage) (any, error) {
	if use, ok := needs[method]; ok && !s.M.May(use) {
		return nil, &Error{CodeNotAllowed, fmt.Sprintf("not allowed: %q is not in the manifest's uses", use)}
	}
	var p struct {
		Root    *Node           `json:"root"`
		Values  map[string]any  `json:"values"`
		Title   string          `json:"title"`
		Body    string          `json:"body"`
		Text    string          `json:"text"`
		OK      string          `json:"ok"`
		Danger  bool            `json:"danger"`
		Target  string          `json:"target"`
		Argv    []string        `json:"argv"`
		Stdin   string          `json:"stdin"`
		Timeout float64         `json:"timeout"`
		Key     string          `json:"key"`
		Value   json.RawMessage `json:"value"`
		ID      string          `json:"id"`
		Seconds float64         `json:"seconds"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, bad(err)
		}
	}
	switch method {
	case "render":
		if p.Root != nil {
			s.host.Render(*p.Root)
		}
		return nil, nil
	case "state.set":
		s.host.SetValues(p.Values)
		return nil, nil
	case "notify":
		return nil, s.notify(p.Title, p.Body)
	case "open":
		if p.Target == "" {
			return nil, bad(errors.New("no target"))
		}
		return nil, exec.Command("xdg-open", p.Target).Start()
	case "clipboard.copy", "clipboard.copySecret":
		return nil, s.host.Copy(p.Text, method == "clipboard.copySecret")
	case "terminal.open":
		return nil, s.terminal(p.Argv)
	case "system.run":
		return s.run(p.Argv, p.Stdin, p.Timeout)
	case "dialog.confirm":
		return s.host.Confirm(context.Background(), p.Title, p.Text, p.OK, p.Danger)
	case "store.get":
		return s.storeGet(p.Key)
	case "store.set":
		return nil, s.storeSet(p.Key, p.Value)
	case "timer.set":
		return nil, s.timer(p.ID, p.Seconds)
	case "window.setTitle":
		s.host.SetTitle(p.Title)
		return nil, nil
	}
	return nil, &Error{CodeNoMethod, "no such method: " + method}
}

// notify shows a desktop notification from the app.
func (s *Session) notify(title, body string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	return conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0, s.M.Name, uint32(0), s.M.IconPath(), title, body,
		[]string{}, map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant(s.M.AppID())}, int32(-1)).Err
}

// terminal runs argv in the terminal set in Settings → Launcher.
func (s *Session) terminal(argv []string) error {
	if len(argv) == 0 {
		return bad(errors.New("no argv"))
	}
	cfg, _, _ := config.Load(config.DefaultPath())
	term := cfg.TerminalCommand()
	if len(term) == 0 {
		return errors.New("no terminal is set (Settings → Launcher)")
	}
	cmd := exec.Command(term[0], append(append(term[1:], "-e"), argv...)...)
	cmd.Dir = s.M.Dir
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// runLimit is how much of a command's output system.run keeps.
const runLimit = 4 << 20

// run runs a command and returns its exit code and output.
func (s *Session) run(argv []string, stdin string, timeout float64) (any, error) {
	if len(argv) == 0 {
		return nil, bad(errors.New("no argv"))
	}
	d := 30 * time.Second
	if timeout > 0 {
		d = min(time.Duration(timeout*float64(time.Second)), 10*time.Minute)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = s.M.Dir
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb limited
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		return nil, err
	}
	return map[string]any{"code": code, "stdout": out.String(), "stderr": errb.String()}, nil
}

// limited keeps the first runLimit bytes written to it.
type limited struct{ bytes.Buffer }

func (l *limited) Write(p []byte) (int, error) {
	if room := runLimit - l.Len(); room > 0 {
		l.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (s *Session) storePath() string { return filepath.Join(DataDir(s.M.ID), "store.json") }

func (s *Session) loadStore() {
	if s.store != nil {
		return
	}
	s.store = map[string]json.RawMessage{}
	if b, err := os.ReadFile(s.storePath()); err == nil {
		json.Unmarshal(b, &s.store)
	}
}

func (s *Session) storeGet(key string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStore()
	if v, ok := s.store[key]; ok {
		return v, nil
	}
	return nil, nil
}

func (s *Session) storeSet(key string, value json.RawMessage) error {
	if key == "" {
		return bad(errors.New("no key"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStore()
	if len(value) == 0 || string(value) == "null" {
		delete(s.store, key)
	} else {
		s.store[key] = value
	}
	b, err := json.MarshalIndent(s.store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.storePath()), 0o700); err != nil {
		return err
	}
	tmp := s.storePath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.storePath())
}

// timer sends a timer event every so many seconds; 0 stops it.
func (s *Session) timer(id string, seconds float64) error {
	if id == "" {
		return bad(errors.New("no id"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if stop, ok := s.timers[id]; ok {
		close(stop)
		delete(s.timers, id)
	}
	if seconds <= 0 {
		return nil
	}
	every := max(time.Duration(seconds*float64(time.Second)), 100*time.Millisecond)
	stop := make(chan struct{})
	s.timers[id] = stop
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.Event("timer", id, nil)
			}
		}
	}()
	return nil
}

func (s *Session) stopTimers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, stop := range s.timers {
		close(stop)
		delete(s.timers, id)
	}
}

// tail keeps the end of what the app writes to stderr, for the window's
// error page, and passes it on to the trace.
type tail struct {
	mu    sync.Mutex
	buf   []byte
	trace io.Writer
}

const tailKeep = 8 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailKeep {
		t.buf = t.buf[len(t.buf)-tailKeep:]
	}
	if t.trace != nil {
		t.trace.Write(p)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
