package desk

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/notify"
)

// --- media (through the shell) --------------------------------------------------

func mediaTools(d Desktop) []Tool {
	return []Tool{
		{
			Name: "media_status", Use: "media", Description: "The music or video player the control center shows: the player, title, artist, whether it plays, and what it can do.",
			Describe: func(map[string]any) string { return i18n.T("Looking at what plays") },
			Run:      func(ctx context.Context, _ map[string]any) (any, error) { return d.shell(ctx, "media", "status") },
		},
		{
			Name: "media_control", Use: "media", Description: "Plays or pauses the player the control center shows, or skips to the next or previous track.",
			Schema: schema(`"action":{"type":"string","enum":["play_pause","next","previous"]}`, "action"),
			Describe: func(a map[string]any) string {
				return map[string]string{"play_pause": i18n.T("Playing or pausing"), "next": i18n.T("Skipping to the next track"),
					"previous": i18n.T("Going back a track")}[str(a, "action")]
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				act := str(a, "action")
				fn, ok := map[string]string{"play_pause": "play-pause", "next": "next", "previous": "previous"}[act]
				if !ok {
					return nil, fmt.Errorf("no action %q", act)
				}
				return d.shell(ctx, "media", fn)
			},
		},
	}
}

// --- the histories ------------------------------------------------------------------

// clipEntry is a clipboard history entry as the tools give it.
type clipEntry struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Cut    bool   `json:"cut,omitempty"` // the text goes on
	Path   string `json:"path,omitempty"`
	MIME   string `json:"mime,omitempty"`
	Source string `json:"source,omitempty"`
	Time   string `json:"time"`
}

// clipID names an entry: when it was copied, which also orders them.
func clipID(e clipboard.Entry) string { return strconv.FormatInt(e.Time.UnixNano(), 36) }

// clipText is how much of an entry's text the list shows.
const clipText = 2000

func (d Desktop) clipFind(id string) (clipboard.Entry, error) {
	es, err := clipboard.Read(d.ClipHistory)
	if err != nil {
		return clipboard.Entry{}, err
	}
	for _, e := range es {
		if clipID(e) == id {
			return e, nil
		}
	}
	return clipboard.Entry{}, fmt.Errorf("no entry %q in the clipboard history", id)
}

func matches(q string, fields ...string) bool {
	return q == "" || strings.Contains(strings.ToLower(strings.Join(fields, " ")), strings.ToLower(q))
}

func limit(a map[string]any, def, most int) int {
	n, _ := num(a, "limit")
	return min(max(cmp.Or(n, def), 1), most)
}

func historyTools(d Desktop) []Tool {
	idArg := `"id":{"type":"string","description":"An entry's id from clipboard_history."}`
	return []Tool{
		{
			Name: "clipboard_history", Use: "clipboard-history", Description: "What was copied, newest first: id, text (long ones cut) or an image's file, the app it came from, when. Passwords marked sensitive are never kept.",
			Schema:   schema(`"query":{"type":"string","description":"Words in the text or the app"},"limit":{"type":"integer","description":"20 by default, up to 100"}`),
			Describe: func(map[string]any) string { return i18n.T("Reading the clipboard history") },
			Run: func(_ context.Context, a map[string]any) (any, error) {
				es, err := clipboard.Read(d.ClipHistory)
				if err != nil {
					return nil, err
				}
				q, n := str(a, "query"), limit(a, 20, 100)
				out := []clipEntry{}
				for _, e := range es {
					if len(out) == n {
						break
					}
					if !matches(q, e.Text, e.Source) {
						continue
					}
					c := clipEntry{ID: clipID(e), Type: e.Type, Path: e.Path, MIME: e.MIME, Source: e.Source, Time: e.Time.Format(time.RFC3339)}
					if r := []rune(e.Text); len(r) > clipText {
						c.Text, c.Cut = string(r[:clipText]), true
					} else {
						c.Text = e.Text
					}
					out = append(out, c)
				}
				return out, nil
			},
		},
		{
			Name: "clipboard_history_copy", Use: "clipboard-history", Description: "Puts an entry of the clipboard history on the clipboard again.",
			Schema: schema(idArg, "id"), Risk: Change,
			Describe: func(map[string]any) string { return i18n.T("Copying from the clipboard history") },
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				e, err := d.clipFind(str(a, "id"))
				if err != nil {
					return nil, err
				}
				if e.Type == "image" {
					if d.ClipImage == nil {
						return nil, errors.New("cannot copy images here")
					}
					return "ok", d.ClipImage(ctx, e.Path, e.MIME)
				}
				return "ok", d.ClipWrite(ctx, e.Text)
			},
		},
		{
			Name: "clipboard_history_delete", Use: "clipboard-history", Description: "Removes an entry from the clipboard history.",
			Schema: schema(idArg, "id"), Risk: Change,
			Describe: func(map[string]any) string { return i18n.T("Removing an entry from the clipboard history") },
			Run: func(_ context.Context, a map[string]any) (any, error) {
				e, err := d.clipFind(str(a, "id"))
				if err != nil {
					return nil, err
				}
				return "ok", clipboard.Delete(d.ClipHistory, e)
			},
		},
		{
			Name: "clipboard_history_clear", Use: "clipboard-history", Description: "Empties the clipboard history.", Risk: Always,
			Describe: func(map[string]any) string { return i18n.T("Clearing the clipboard history") },
			Run: func(context.Context, map[string]any) (any, error) {
				return "ok", clipboard.Clear(d.ClipHistory)
			},
		},
		{
			Name: "notification_history", Use: "notifications", Description: "The notifications that came, newest first, also those dismissed: id, app, summary, body, time. Kept only when the user turned it on in Settings.",
			Schema:   schema(`"query":{"type":"string","description":"Words in the app, summary or body"},"limit":{"type":"integer","description":"20 by default, up to 200"}`),
			Describe: func(map[string]any) string { return i18n.T("Reading the notification history") },
			Run: func(_ context.Context, a map[string]any) (any, error) {
				if d.NoteHistoryOn != nil && !d.NoteHistoryOn() {
					return nil, errors.New("the notification history is off (Settings → Notifications → Keep a history)")
				}
				rs, err := notify.ReadHistory(d.NoteHistory)
				if err != nil {
					return nil, err
				}
				q, n := str(a, "query"), limit(a, 20, 200)
				out := []notify.Record{}
				for _, r := range rs {
					if len(out) == n {
						break
					}
					if matches(q, r.App, r.Summary, r.Body) {
						out = append(out, r)
					}
				}
				return out, nil
			},
		},
	}
}
