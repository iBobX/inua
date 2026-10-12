package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// APIBase is the repository in GitHub's API, where release notes are read
// (Latest does not use it, so the daily check has no rate limit to mind).
var APIBase = "https://api.github.com/repos/lyricat/inua"

// Release is a published release and its notes.
type Release struct {
	Tag  string    `json:"tag_name"`
	Name string    `json:"name"`
	Body string    `json:"body"`
	Date time.Time `json:"published_at"`
	URL  string    `json:"html_url"`
}

// Releases reads the latest n releases, newest first.
func (s Source) Releases(ctx context.Context, n int) ([]Release, error) {
	base := s.API
	if base == "" {
		base = APIBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/releases?per_page=%d", base, n), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading the releases: %s", resp.Status)
	}
	var out []Release
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("reading the releases: %w", err)
	}
	return out, nil
}

var commitHash = regexp.MustCompile(`^[0-9a-f]{7,40}\s+`)

// Changes are a release's notes as a list of changes: the notes' bullets,
// without the commit hashes GoReleaser puts first; headings, the install
// line and blank lines are left out. Notes without bullets come back as
// their lines.
func Changes(body string) []string {
	var bullets, lines []string
	for l := range strings.Lines(body) {
		l = strings.TrimSpace(l)
		switch {
		case l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "Install"):
		case strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "- "):
			bullets = append(bullets, commitHash.ReplaceAllString(strings.TrimSpace(l[2:]), ""))
		default:
			lines = append(lines, strings.ReplaceAll(l, "`", ""))
		}
	}
	if len(bullets) > 0 {
		return bullets
	}
	return lines
}
