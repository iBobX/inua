package filesui

import (
	"path/filepath"

	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/i18n"
)

// kind is what a file is, for its icon and the list's Kind column.
//
// Files, folders, places and drives are drawn with the desktop's icon
// theme, in colour (the theme the XDG settings portal names, which the
// apps' Phosphor theme inherits): GIO's icon for a MIME type names the
// theme's specific icon and its generic one, and a theme that lacks one
// falls back to the next. The window's buttons keep the shell's icons.
type kind struct {
	mime, desc string
	icon       string        // a theme icon's name, when gicon is nil
	gicon      *gio.IconBase // GIO's icon for the type
}

// kinds caches GIO's guess for each extension: the guess reads only the
// name (shared-mime-info's globs), and a row is bound often.
type kinds map[string]kind

func (k kinds) of(e *files.Entry) kind {
	switch {
	case e.Broken:
		return kind{mime: "inode/symlink", desc: i18n.T("Broken link"), icon: "emblem-unreadable"}
	case e.Dir:
		return kind{mime: "inode/directory", desc: i18n.T("Folder"), icon: "folder"}
	}
	ext := e.Ext()
	if ext == "" && e.Mode&0o111 != 0 {
		return kind{mime: "application/x-executable", desc: i18n.T("Program"), icon: "application-x-executable"}
	}
	if c, ok := k[ext]; ok {
		return c
	}
	var uncertain bool
	mime := gio.ContentTypeGuess(e.Name, nil, 0, &uncertain)
	c := kind{mime: mime, desc: gio.ContentTypeGetDescription(mime), gicon: gio.ContentTypeGetIcon(mime)}
	if ext != "" {
		k[ext] = c
	}
	return c
}

// folderIcon is a folder's icon: home's, the user folders' own, the
// generic folder's for the rest.
func folderIcon(path string) string {
	if path == files.Home() {
		return "user-home"
	}
	for key, dir := range files.UserDirs() {
		if dir == path {
			if icon, ok := userDirIcons[key]; ok {
				return icon
			}
		}
	}
	if filepath.Dir(path) == path {
		return "drive-harddisk"
	}
	return "folder"
}

var userDirIcons = map[string]string{
	"DESKTOP": "user-desktop", "DOCUMENTS": "folder-documents", "DOWNLOAD": "folder-download",
	"MUSIC": "folder-music", "PICTURES": "folder-pictures", "VIDEOS": "folder-videos",
	"TEMPLATES": "folder-templates", "PUBLICSHARE": "folder-publicshare",
}
