package connect

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/kdeconnect"
)

// shares are files, text and links shared with the devices, and from
// them: files go to Downloads, text to the clipboard, links open.
type shares struct{ s *Service }

func newShares(s *Service) *shares { return &shares{s: s} }

// shareBody is a share packet's body.
type shareBody struct {
	Filename         string `json:"filename,omitempty"`
	LastModified     int64  `json:"lastModified,omitempty"`
	CreationTime     int64  `json:"creationTime,omitempty"`
	Open             bool   `json:"open,omitempty"`
	Text             string `json:"text,omitempty"`
	URL              string `json:"url,omitempty"`
	NumberOfFiles    int    `json:"numberOfFiles,omitempty"`
	TotalPayloadSize int64  `json:"totalPayloadSize,omitempty"`
}

// Downloads is the folder files shared from devices are put in.
func Downloads() string {
	if d := files.UserDirs()["DOWNLOAD"]; d != "" {
		return d
	}
	return filepath.Join(files.Home(), "Downloads")
}

func (sh *shares) received(d kdeconnect.Device, p kdeconnect.Packet) {
	var b shareBody
	if p.Decode(&b) != nil {
		return
	}
	switch {
	case p.PayloadTransferInfo != nil:
		go sh.receiveFile(d, p, b)
	case b.URL != "":
		u, err := url.Parse(b.URL)
		if err != nil || u.Scheme == "" || u.Scheme == "file" {
			return
		}
		if err := exec.Command("xdg-open", b.URL).Start(); err != nil {
			log.Printf("connect: open %s: %v", b.URL, err)
		}
	case b.Text != "":
		sh.s.clip.received(kdeconnect.NewPacket(typeClipboard, map[string]string{"content": b.Text}))
		sh.s.notes.Post(Note{App: d.Name, Icon: "edit-paste-symbolic", Title: i18n.Tf("Text from %s", d.Name),
			Body: i18n.T("It is on the clipboard.")})
	}
}

// receiveFile fetches a file shared and puts it in Downloads, under a
// name no other file there has.
func (sh *shares) receiveFile(d kdeconnect.Device, p kdeconnect.Packet, b shareBody) {
	name := safeName(b.Filename)
	dir := Downloads()
	err := func() error {
		r, err := sh.s.core.Payload(sh.s.ctx, d.ID, p)
		if err != nil {
			return err
		}
		defer r.Close()
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		f, err := os.CreateTemp(dir, ".inua-connect-*")
		if err != nil {
			return err
		}
		n, err := io.Copy(f, r)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil && n != p.PayloadSize {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			os.Remove(f.Name())
			return err
		}
		// A file shared is like one downloaded: not the temporary file's 0600.
		os.Chmod(f.Name(), 0666&^umask())
		path := freeName(dir, name)
		if err := os.Rename(f.Name(), path); err != nil {
			os.Remove(f.Name())
			return err
		}
		if b.LastModified > 0 {
			t := time.UnixMilli(b.LastModified)
			os.Chtimes(path, t, t)
		}
		name = path
		return nil
	}()
	if err != nil {
		log.Printf("connect: a file from %s: %v", d.Name, err)
		sh.s.notes.Post(Note{App: d.Name, Icon: "dialog-error-symbolic", Title: i18n.Tf("A file from %s did not arrive", d.Name),
			Body: safeName(b.Filename)})
		return
	}
	path := name
	if b.Open {
		exec.Command("xdg-open", path).Start()
		return
	}
	sh.s.notes.Post(Note{
		App:     d.Name,
		Icon:    "folder-download-symbolic",
		Title:   i18n.Tf("A file from %s", d.Name),
		Body:    filepath.Base(path),
		Actions: []Action{{"default", i18n.T("Open")}, {"folder", i18n.T("Show in Folder")}},
		Acted: func(key string) {
			target := path
			if key == "folder" {
				target = filepath.Dir(path)
			}
			exec.Command("xdg-open", target).Start()
		},
	})
}

// umask is the process's file mode mask, read without changing it (022
// when it cannot be read).
func umask() os.FileMode {
	data, _ := os.ReadFile("/proc/self/status")
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "Umask:"); ok {
			if m, err := strconv.ParseUint(strings.TrimSpace(v), 8, 32); err == nil {
				return os.FileMode(m)
			}
		}
	}
	return 0o022
}

// safeName is a file's name as a device gave it, kept to a name in the
// folder: no folders, no hidden file.
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" || name == "/" {
		return i18n.T("Shared file")
	}
	return name
}

// freeName is name in dir, or "name (2).ext" and so on when it is taken.
func freeName(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		path := filepath.Join(dir, n)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return path
		}
	}
}

// sendFiles sends files to the device, one after another; a note says
// when they are there.
func (sh *shares) sendFiles(id string, paths []string) error {
	d, ok := sh.s.core.Device(id)
	if !ok || !d.Reachable || d.Pair != kdeconnect.Paired {
		return kdeconnect.ErrNotPaired
	}
	var total int64
	var infos []os.FileInfo
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("connect: %s is not a file", p)
		}
		total += fi.Size()
		infos = append(infos, fi)
	}
	go func() {
		sent := 0
		for i, p := range paths {
			f, err := os.Open(p)
			if err == nil {
				body := shareBody{Filename: filepath.Base(p), LastModified: infos[i].ModTime().UnixMilli(),
					NumberOfFiles: len(paths), TotalPayloadSize: total}
				err = sh.s.core.SendPayload(sh.s.ctx, id, kdeconnect.NewPacket(typeShare, body), f, infos[i].Size())
				f.Close()
			}
			if err != nil {
				log.Printf("connect: sending %s to %s: %v", p, d.Name, err)
				sh.s.notes.Post(Note{App: i18n.T("Mobile Link"), Icon: "dialog-error-symbolic",
					Title: i18n.Tf("%s did not get %s", d.Name, filepath.Base(p)), Body: err.Error()})
				return
			}
			sent++
		}
		sh.s.notes.Post(Note{App: i18n.T("Mobile Link"), Icon: DeviceIcon(d.Type),
			Title: i18n.Nf("Sent %d file to %s", "Sent %d files to %s", sent, sent, d.Name)})
	}()
	return nil
}

// sendText sends the device text, or a link, which it opens.
func (sh *shares) sendText(id, text string) error {
	text = strings.TrimSpace(text)
	if u, err := url.Parse(text); err == nil && (u.Scheme == "http" || u.Scheme == "https") && !strings.ContainsAny(text, " \n") {
		return sh.s.core.Send(id, kdeconnect.NewPacket(typeShare, shareBody{URL: text}))
	}
	return sh.s.core.Send(id, kdeconnect.NewPacket(typeShare, shareBody{Text: text}))
}
