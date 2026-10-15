package filesui

import (
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/i18n"
)

// kind is what a file is, for its icon and the list's Kind column.
type kind struct {
	mime, desc, icon string
}

// kinds caches GIO's guess for each extension: the guess reads only the
// name (shared-mime-info's globs), and a row is bound often.
type kinds map[string]kind

func (k kinds) of(e *files.Entry) kind {
	switch {
	case e.Broken:
		return kind{"inode/symlink", i18n.T("Broken link"), "emblem-symbolic-link-symbolic"}
	case e.Dir:
		return kind{"inode/directory", i18n.T("Folder"), "folder-symbolic"}
	}
	ext := e.Ext()
	if ext == "" && e.Mode&0o111 != 0 {
		return kind{"application/x-executable", i18n.T("Program"), "application-x-executable-symbolic"}
	}
	if c, ok := k[ext]; ok {
		return c
	}
	var uncertain bool
	mime := gio.ContentTypeGuess(e.Name, nil, 0, &uncertain)
	c := kind{mime: mime, desc: gio.ContentTypeGetDescription(mime), icon: iconFor(mime)}
	if ext != "" {
		k[ext] = c
	}
	return c
}

// iconFor is the shell's icon for a MIME type: a few kinds, drawn from
// Phosphor (ui/icons), rather than the theme's many.
func iconFor(mime string) string {
	top, sub, _ := strings.Cut(mime, "/")
	switch top {
	case "image":
		return "image-x-generic-symbolic"
	case "audio":
		return "audio-x-generic-symbolic"
	case "video":
		return "video-x-generic-symbolic"
	case "font":
		return "font-x-generic-symbolic"
	case "text":
		switch sub {
		case "plain", "markdown", "x-readme", "x-log", "csv":
			return "text-x-generic-symbolic"
		}
		return "text-x-script-symbolic"
	}
	switch {
	case sub == "pdf":
		return "application-pdf-symbolic"
	case strings.Contains(sub, "zip"), strings.Contains(sub, "tar"), strings.Contains(sub, "compressed"),
		strings.Contains(sub, "rar"), strings.Contains(sub, "7z"), strings.Contains(sub, "xz"),
		strings.Contains(sub, "bzip"), strings.Contains(sub, "zstd"), strings.Contains(sub, "archive"),
		strings.Contains(sub, "deb"), strings.Contains(sub, "rpm"):
		return "package-x-generic-symbolic"
	case strings.Contains(sub, "spreadsheet"), strings.Contains(sub, "excel"):
		return "x-office-spreadsheet-symbolic"
	case strings.Contains(sub, "presentation"), strings.Contains(sub, "powerpoint"):
		return "x-office-presentation-symbolic"
	case strings.Contains(sub, "document"), strings.Contains(sub, "msword"), strings.Contains(sub, "rtf"),
		strings.Contains(sub, "epub"):
		return "x-office-document-symbolic"
	case strings.Contains(sub, "iso9660"), strings.Contains(sub, "disk-image"), strings.Contains(sub, "cd-image"):
		return "media-optical-symbolic"
	case strings.Contains(sub, "executable"), strings.Contains(sub, "sharedlib"), strings.Contains(sub, "appimage"):
		return "application-x-executable-symbolic"
	case strings.Contains(sub, "json"), strings.Contains(sub, "xml"), strings.Contains(sub, "javascript"),
		strings.Contains(sub, "shellscript"), strings.Contains(sub, "x-python"), strings.Contains(sub, "toml"),
		strings.Contains(sub, "yaml"), strings.Contains(sub, "sql"):
		return "text-x-script-symbolic"
	}
	return "application-x-generic-symbolic"
}
