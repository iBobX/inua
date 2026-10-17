package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// section is the name of the one remote in the config file rclone is given.
const section = "disk"

// obscured are the kinds whose password rclone takes obscured (its
// reversible encoding, `rclone obscure`), not as it is.
var obscured = map[Kind]bool{SFTP: true, SMB: true, FTP: true, WebDAV: true}

// option is one line of a remote's section.
type option struct{ key, value string }

// options are rclone's settings for an address, the secret aside.
func options(a Address, home string) []option {
	opt := func(k, v string) option { return option{k, v} }
	var out []option
	add := func(o ...option) {
		for _, x := range o {
			if x.value != "" {
				out = append(out, x)
			}
		}
	}
	switch a.Kind {
	case SFTP:
		add(opt("type", "sftp"), opt("host", a.Host), opt("user", a.User), opt("port", a.Port))
		// rclone checks the host's key only against a file it is given;
		// the user's own is what ssh checks it against.
		add(opt("known_hosts_file", filepath.Join(home, ".ssh", "known_hosts")))
	case SMB:
		add(opt("type", "smb"), opt("host", a.Host), opt("user", a.User), opt("port", a.Port))
	case FTP:
		user := a.User
		if user == "" {
			user = "anonymous"
		}
		add(opt("type", "ftp"), opt("host", a.Host), opt("user", user), opt("port", a.Port))
		if a.Scheme == "ftps" {
			add(opt("tls", "true"))
		}
	case WebDAV:
		u := "https://"
		if a.Scheme == "dav" {
			u = "http://"
		}
		u += a.Host
		if a.Port != "" {
			u += ":" + a.Port
		}
		if a.Path != "" {
			u += "/" + a.Path
		}
		add(opt("type", "webdav"), opt("url", u), opt("vendor", "other"), opt("user", a.User))
	case S3:
		add(opt("type", "s3"), opt("env_auth", "false"), opt("access_key_id", a.User))
		if region, ok := awsRegion(a.Host); ok {
			add(opt("provider", "AWS"), opt("region", region))
		} else {
			endpoint := "https://" + a.Host
			if a.Port != "" {
				endpoint += ":" + a.Port
			}
			add(opt("provider", "Other"), opt("endpoint", endpoint))
		}
		// A key may be for one bucket and not let rclone ask for others.
		add(opt("no_check_bucket", "true"))
	case R2:
		add(opt("type", "s3"), opt("provider", "Cloudflare"), opt("env_auth", "false"),
			opt("access_key_id", a.User), opt("endpoint", "https://"+a.Host+".r2.cloudflarestorage.com"),
			opt("region", "auto"), opt("acl", "private"), opt("no_check_bucket", "true"))
	}
	return out
}

// awsRegion is the region of an AWS S3 host: s3.amazonaws.com is
// us-east-1, s3.eu-west-1.amazonaws.com is eu-west-1.
func awsRegion(host string) (string, bool) {
	rest, ok := strings.CutSuffix(host, ".amazonaws.com")
	if !ok {
		return "", false
	}
	switch {
	case rest == "s3":
		return "us-east-1", true
	case strings.HasPrefix(rest, "s3."), strings.HasPrefix(rest, "s3-"):
		return rest[3:], true
	}
	return "", false
}

// secretKey is the option that holds the secret.
func secretKey(k Kind) string {
	if k == S3 || k == R2 {
		return "secret_access_key"
	}
	return "pass"
}

// target is what rclone mounts: the remote and the folder in it.
func target(a Address) string {
	if a.Kind == WebDAV {
		return section + ":" // the folder is in the URL
	}
	return section + ":" + a.Path
}

// config is the config file rclone is given for an address: its section,
// with the secret (obscured where rclone wants it so) when there is one.
func config(a Address, secret, home string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "[%s]\n", section)
	for _, o := range options(a, home) {
		fmt.Fprintf(&b, "%s = %s\n", o.key, o.value)
	}
	if secret != "" {
		fmt.Fprintf(&b, "%s = %s\n", secretKey(a.Kind), secret)
	}
	return b.Bytes()
}

// Engine connects and disconnects disks: rclone mounts each one, run by
// the user's systemd as a unit of its own, so that a disk stays mounted
// when the window closes, and goes when the session ends.
type Engine struct {
	// Run runs a program, with stdin, and returns what it printed.
	Run func(ctx context.Context, stdin, name string, args ...string) ([]byte, error)
	// Probe runs rclone and reports whether it lists something, or
	// nothing, without its error; it stops at the first line.
	Probe func(ctx context.Context, name string, args ...string) error
	// Mounts are the mount table's mount points.
	Mounts   func() ([]string, error)
	LookPath func(string) (string, error)
	Getenv   func(string) string
	// Home, and the folders for the disks' runtime files and caches.
	Home, Runtime, Cache, Dir string
	Poll                      time.Duration
}

// New is the engine of the user's session, reading the mount table with
// mounts.
func New(mounts func() ([]string, error)) *Engine {
	home, _ := os.UserHomeDir()
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = os.TempDir()
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	return &Engine{
		Run: run, Probe: probe, Mounts: mounts, LookPath: exec.LookPath, Getenv: os.Getenv,
		Home: home, Runtime: filepath.Join(runtime, "inua", "remote"), Cache: filepath.Join(cache, "inua", "remote"),
		Dir: Dir(), Poll: 200 * time.Millisecond,
	}
}

func run(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, bytes.TrimSpace(out))
	}
	return out, nil
}

func probe(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, lerr := bufio.NewReader(out).ReadString('\n')
	listed := lerr == nil
	if listed {
		cancel() // one name is enough: the rest of a bucket can be long
	}
	io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil && !listed {
		return &Error{Reason: Classify(stderr.String()), Detail: lastLine(stderr.String())}
	}
	return nil
}

// Point is where a disk is mounted.
func (e *Engine) Point(d Disk) string { return filepath.Join(e.Dir, d.Name) }

// Mounted says whether a disk is mounted now.
func (e *Engine) Mounted(d Disk) bool {
	ms, err := e.Mounts()
	return err == nil && slices.Contains(ms, e.Point(d))
}

// Ready reports the first program connecting needs that is not there:
// rclone, FUSE's fusermount3, systemd's systemd-run.
func (e *Engine) Ready() error {
	for _, p := range []struct{ bin, pkg string }{{"rclone", "rclone"}, {"fusermount3", "fuse3"}, {"systemd-run", "systemd"}} {
		if _, err := e.LookPath(p.bin); err != nil {
			return &Error{Reason: Missing, Detail: p.pkg}
		}
	}
	return nil
}

// Connect mounts d with its secret ("" for none) and returns when its
// folder is there. It tries the address first, so that a refused password
// or a wrong bucket is said at once, not later as a folder that does not
// open.
func (e *Engine) Connect(ctx context.Context, d Disk, secret string) (err error) {
	if err := CheckName(d.Name); err != nil {
		return err
	}
	a, err := Parse(d.Address)
	if err != nil {
		return err
	}
	if err := e.Ready(); err != nil {
		return err
	}
	rclone, _ := e.LookPath("rclone")
	point := e.Point(d)
	if e.Mounted(d) {
		return nil
	}
	if err := os.MkdirAll(point, 0700); err != nil {
		return err
	}
	if es, err := os.ReadDir(point); err != nil || len(es) > 0 {
		return &Error{Reason: Occupied, Detail: point}
	}
	defer func() {
		if err != nil {
			os.Remove(point) // the folder made for it, when nothing came of it
		}
	}()
	unit := unitName(d.Name)
	conf, err := e.writeConfig(ctx, rclone, a, secret, unit+".conf")
	if err != nil {
		return err
	}
	defer os.Remove(conf)
	if err := e.list(ctx, rclone, conf, a); err != nil {
		return err
	}
	logFile := filepath.Join(e.Runtime, unit+".log")
	os.Remove(logFile)
	e.Run(ctx, "", "systemctl", "--user", "reset-failed", unit) // a failed run before keeps the name
	args := []string{"--user", "--unit=" + unit, "--description=inua remote disk " + d.Name, "--collect", "--quiet"}
	// The unit starts in the user manager's environment, not this one:
	// the SSH agent SFTP signs in with, and where fusermount3 is.
	for _, k := range []string{"SSH_AUTH_SOCK", "PATH"} {
		if v := e.Getenv(k); v != "" {
			args = append(args, "--setenv="+k+"="+v)
		}
	}
	args = append(args, "--",
		rclone, "mount", target(a), point,
		"--config", conf,
		"--cache-dir", e.cacheDir(d),
		"--vfs-cache-mode", "writes",
		"--dir-cache-time", "1m",
		"--volname", d.Name,
		"--log-file", logFile, "--log-level", "NOTICE",
	)
	if _, err := e.Run(ctx, "", "systemd-run", args...); err != nil {
		return err
	}
	for {
		if e.Mounted(d) {
			return nil
		}
		if out, _ := e.Run(ctx, "", "systemctl", "--user", "is-active", unit); !isActive(out) {
			log, _ := os.ReadFile(logFile)
			return &Error{Reason: Classify(string(log)), Detail: lastLine(string(log))}
		}
		select {
		case <-ctx.Done():
			e.Disconnect(context.Background(), d)
			return ctx.Err()
		case <-time.After(e.Poll):
		}
	}
}

// Test tries d as Connect does first, listing its folder with its secret,
// and mounts nothing: whether it would connect, and if not, why.
func (e *Engine) Test(ctx context.Context, d Disk, secret string) error {
	a, err := Parse(d.Address)
	if err != nil {
		return err
	}
	if err := e.Ready(); err != nil {
		return err
	}
	rclone, _ := e.LookPath("rclone")
	conf, err := e.writeConfig(ctx, rclone, a, secret, unitName(d.Name)+".test.conf")
	if err != nil {
		return err
	}
	defer os.Remove(conf)
	return e.list(ctx, rclone, conf, a)
}

// writeConfig writes the config rclone is given for a, with its secret
// (obscured where rclone wants it so), as name in the runtime folder; the
// caller removes it. It holds the secret: only while it is needed, and
// only for the user (the runtime folder is theirs alone, and in memory).
func (e *Engine) writeConfig(ctx context.Context, rclone string, a Address, secret, name string) (string, error) {
	if err := os.MkdirAll(e.Runtime, 0700); err != nil {
		return "", err
	}
	if secret != "" && obscured[a.Kind] {
		out, err := e.Run(ctx, secret, rclone, "obscure", "-")
		if err != nil {
			return "", err
		}
		secret = strings.TrimSpace(string(out))
	}
	conf := filepath.Join(e.Runtime, name)
	return conf, os.WriteFile(conf, config(a, secret, e.Home), 0600)
}

// list lists the address's folder, stopping at the first name: a refused
// password, an unknown host key or a missing bucket is said here.
func (e *Engine) list(ctx context.Context, rclone, conf string, a Address) error {
	return e.Probe(ctx, rclone, "lsf", "--config", conf, "--max-depth", "1", "--low-level-retries", "1", "--retries", "1", target(a))
}

func isActive(out []byte) bool {
	s := strings.TrimSpace(string(out))
	return s == "active" || s == "activating"
}

// Disconnect unmounts d: its rclone is stopped, which writes what it still
// holds and unmounts; a mount left behind (rclone gone) is taken away.
// Its empty folder goes too.
func (e *Engine) Disconnect(ctx context.Context, d Disk) error {
	if err := CheckName(d.Name); err != nil {
		return err
	}
	point := e.Point(d)
	e.Run(ctx, "", "systemctl", "--user", "stop", unitName(d.Name))
	if e.Mounted(d) {
		if _, err := e.Run(ctx, "", "fusermount3", "-u", point); err != nil {
			return &Error{Reason: Busy, Detail: lastLine(err.Error())}
		}
	}
	os.Remove(point) // only when it is empty
	return nil
}

// cacheDir is where rclone keeps a disk's files while they are written.
// It is the address's, not the name's: what was written and not yet sent
// when rclone stopped is sent the next time the same server is mounted,
// and never to another disk given the name later.
func (e *Engine) cacheDir(d Disk) string {
	sum := sha256.Sum256([]byte(d.Address))
	return filepath.Join(e.Cache, hex.EncodeToString(sum[:8]))
}

// Forget takes away a disk's empty folder; a mounted disk is disconnected
// first. Its cache stays, for what it has not sent yet; the keyring's
// secret is the caller's.
func (e *Engine) Forget(d Disk) error {
	if err := CheckName(d.Name); err != nil {
		return err
	}
	if e.Mounted(d) {
		return &Error{Reason: Busy, Detail: e.Point(d)}
	}
	os.Remove(e.Point(d)) // only when it is empty
	return nil
}

// Reason is why connecting or disconnecting failed, for the window to say.
type Reason int

const (
	Other       Reason = iota
	Missing            // a program is not installed; Detail is its package
	Refused            // the user, password or key was refused
	HostKey            // the SSH host's key is not known, or not the one known
	Unreachable        // the server does not answer
	NotFound           // the share, bucket or folder is not there
	Occupied           // the disk's folder has files in it
	Busy               // something has a file open on the disk
	Old                // rclone is too old for the kind
)

// Error is a failure with its reason, and rclone's own last words.
type Error struct {
	Reason Reason
	Detail string
}

func (e *Error) Error() string { return e.Detail }

// AsError is err's Error, if it is one.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// Classify reads rclone's error output for why it failed.
func Classify(log string) Reason {
	l := strings.ToLower(log)
	has := func(s ...string) bool {
		return slices.ContainsFunc(s, func(x string) bool { return strings.Contains(l, x) })
	}
	switch {
	case has("knownhosts", "host key"):
		return HostKey
	case has("unable to authenticate", "logon_failure", "login incorrect", "530 ", "401 unauthorized",
		"invalidaccesskeyid", "signaturedoesnotmatch", "accessdenied", "403 forbidden", "authentication failed",
		"status_access_denied"):
		return Refused
	case has("no such host", "connection refused", "i/o timeout", "network is unreachable", "no route to host",
		"connection timed out"):
		return Unreachable
	case has("nosuchbucket", "directory not found", "bad_network_name", "object not found", "404 not found"):
		return NotFound
	case has("didn't find backend called"):
		return Old
	}
	return Other
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
