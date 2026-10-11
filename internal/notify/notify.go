// Package notify is the shell's notification service: the freedesktop
// notification server (org.freedesktop.Notifications, Serve) and the model
// of the notification center (Center), which works like iOS's. A new
// notification shows as a banner for a moment, then waits in the center,
// grouped by app, until the user opens or clears it. Focus keeps banners
// quiet; notifications still collect in the center.
//
// Spec: https://specifications.freedesktop.org/notification-spec/latest/
package notify

import (
	"regexp"
	"strings"
	"time"
)

// Urgency is the urgency hint.
type Urgency uint8

const (
	Low Urgency = iota
	Normal
	Critical
)

// Action is a button the app offers. The "default" action is not one: it
// is what clicking the notification itself does (Notification.Default).
type Action struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Image is the image-data hint: raw pixels, 8 bits per sample.
type Image struct {
	Width, Height, Stride int
	Alpha                 bool // RGBA, else RGB
	Data                  []byte
}

// MaxImageSide is the largest width or height of an image-data hint kept;
// a notification shows it at 44 pixels.
const MaxImageSide = 4096

// Pixels are the bytes the image's rows take, nil when its sizes do not
// add up: rows of Stride bytes, the last only Width pixels long.
func (img *Image) Pixels() []byte {
	if img == nil || img.Width <= 0 || img.Height <= 0 || img.Width > MaxImageSide || img.Height > MaxImageSide {
		return nil
	}
	px := int64(3)
	if img.Alpha {
		px = 4
	}
	row, stride := int64(img.Width)*px, int64(img.Stride)
	if stride < row || stride > row+4096 { // rows are padded a little at most
		return nil
	}
	size := stride*int64(img.Height-1) + row
	if size > int64(len(img.Data)) {
		return nil
	}
	return img.Data[:size]
}

// Notification is one notification as the app sent it.
type Notification struct {
	ID      uint32    `json:"id"`
	App     string    `json:"app"`      // app_name
	Entry   string    `json:"entry"`    // desktop-entry hint: the app's .desktop ID
	Icon    string    `json:"icon"`     // app_icon: an icon name, a path or a file:// URI
	Summary string    `json:"summary"`  // plain text
	Body    string    `json:"body"`     // Pango markup, from Markup
	Actions []Action  `json:"actions"`  // buttons, without "default"
	Default bool      `json:"default"`  // the app has a "default" action
	Urgency Urgency   `json:"urgency"`  // urgency hint
	Time    time.Time `json:"time"`     // when it arrived
	Sender  string    `json:"-"`        // the app's bus name, for its signals
	Image   *Image    `json:"-"`        // image-data hint
	ImgPath string    `json:"img_path"` // image-path hint, a path or file:// URI

	// Transient notifications skip the center; resident ones stay after an
	// action.
	Transient bool `json:"-"`
	Resident  bool `json:"resident"`
}

// Key groups notifications in the center: the desktop entry, else the app
// name.
func (n *Notification) Key() string {
	if n.Entry != "" {
		return n.Entry
	}
	return n.App
}

// Close reasons for the NotificationClosed signal.
const (
	Expired   uint32 = 1
	Dismissed uint32 = 2
	Closed    uint32 = 3 // by CloseNotification
)

// Markup turns a notification body into Pango markup a GtkLabel accepts.
// The spec allows <b>, <i>, <u> and <a href>; apps also send plain text with
// bare & and <, and unbalanced or unknown tags. Allowed tags are kept and
// balanced, entities are kept, and everything else is escaped.
func Markup(body string) string {
	var b strings.Builder
	var open []string // tags open, innermost last
	for i := 0; i < len(body); {
		c := body[i]
		switch c {
		case '<':
			m := tagRe.FindStringSubmatch(body[i:])
			if m == nil {
				b.WriteString("&lt;")
				i++
				continue
			}
			closing, name, href := m[1] == "/", strings.ToLower(m[2]), m[3]
			i += len(m[0])
			if name == "img" || name == "br" {
				if name == "br" {
					b.WriteByte('\n')
				}
				continue // images are not shown inline
			}
			if !closing {
				open = append(open, name)
				if name == "a" {
					b.WriteString(`<a href="` + attrEscape(href) + `">`)
				} else {
					b.WriteString("<" + name + ">")
				}
				continue
			}
			// Close up to the matching tag; a closer with no opener is
			// dropped.
			at := -1
			for j := len(open) - 1; j >= 0; j-- {
				if open[j] == name {
					at = j
					break
				}
			}
			if at < 0 {
				continue
			}
			for j := len(open) - 1; j >= at; j-- {
				b.WriteString("</" + open[j] + ">")
			}
			open = open[:at]
		case '&':
			if m := entityRe.FindString(body[i:]); m != "" {
				b.WriteString(m)
				i += len(m)
				continue
			}
			b.WriteString("&amp;")
			i++
		case '>':
			b.WriteString("&gt;")
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	for j := len(open) - 1; j >= 0; j-- {
		b.WriteString("</" + open[j] + ">")
	}
	return b.String()
}

var (
	tagRe    = regexp.MustCompile(`^<(/?)(?i:(b|i|u|a|img|br))(?:\s+href\s*=\s*"([^"<>]*)")?(?:\s+[a-zA-Z-]+\s*=\s*"[^"<>]*")*\s*/?>`)
	entityRe = regexp.MustCompile(`^&(?:amp|lt|gt|quot|apos|#[0-9]{1,7}|#x[0-9a-fA-F]{1,6});`)
)

func attrEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// PlainText is markup with the tags dropped and entities decoded, for
// places that show one line.
func PlainText(markup string) string {
	s := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(markup, "")
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
}
