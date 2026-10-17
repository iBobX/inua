package files

import (
	"net/url"
	"strings"
)

// The clipboard carries files as text/uri-list (RFC 2483), which every
// app reads, and as x-special/gnome-copied-files, which says whether they
// were copied or cut, as Nautilus, Thunar, Dolphin and GTK's file chooser
// read it: "copy" or "cut", then a URI a line.

// URIList writes paths as text/uri-list.
func URIList(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(URIOfPath(p))
		b.WriteString("\r\n")
	}
	return b.String()
}

// GnomeCopied writes paths as x-special/gnome-copied-files.
func GnomeCopied(paths []string, cut bool) string {
	var b strings.Builder
	if cut {
		b.WriteString("cut")
	} else {
		b.WriteString("copy")
	}
	for _, p := range paths {
		b.WriteByte('\n')
		b.WriteString(URIOfPath(p))
	}
	return b.String()
}

// ParseClipboard reads either format (or plain text of paths, one a line)
// into local paths, and whether they were cut. Lines that are not local
// files are left out.
func ParseClipboard(s string) (paths []string, cut bool) {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) > 0 {
		switch strings.TrimSpace(lines[0]) {
		case "cut":
			cut = true
			lines = lines[1:]
		case "copy":
			lines = lines[1:]
		}
	}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "~/") {
			l = Home() + l[1:]
		}
		if p, ok := PathOfURI(l); ok {
			paths = append(paths, p)
		}
	}
	return paths, cut
}

// IsLocalURI says whether s names a local file rather than a network
// location: a path, absolute or relative (`open .`, `open Downloads`), or
// a file:// URI.
func IsLocalURI(s string) bool {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") {
		return true
	}
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		return u.Scheme == "file"
	}
	return true // no scheme: a path, relative to where it was given
}
