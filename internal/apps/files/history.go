package files

import (
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// History is a pane's back and forward lists.
type History struct {
	back, fwd []string
}

// Visit records leaving from for another folder; going somewhere new
// forgets what was forward.
func (h *History) Visit(from string) {
	if from == "" {
		return
	}
	if n := len(h.back); n == 0 || h.back[n-1] != from {
		h.back = append(h.back, from)
	}
	if len(h.back) > 200 {
		h.back = h.back[len(h.back)-200:]
	}
	h.fwd = h.fwd[:0]
}

// Back returns the folder before cur and moves cur forward.
func (h *History) Back(cur string) (string, bool) {
	if len(h.back) == 0 {
		return "", false
	}
	p := h.back[len(h.back)-1]
	h.back = h.back[:len(h.back)-1]
	h.fwd = append(h.fwd, cur)
	return p, true
}

// Forward undoes Back.
func (h *History) Forward(cur string) (string, bool) {
	if len(h.fwd) == 0 {
		return "", false
	}
	p := h.fwd[len(h.fwd)-1]
	h.fwd = h.fwd[:len(h.fwd)-1]
	h.back = append(h.back, cur)
	return p, true
}

// CanBack and CanForward say whether there is somewhere to go.
func (h *History) CanBack() bool    { return len(h.back) > 0 }
func (h *History) CanForward() bool { return len(h.fwd) > 0 }

// Jumps are the folders used, for jumping back to them by a few letters:
// zoxide's frecency, each visit adding to a folder's rank and the ranks
// aging as the total grows.
type Jumps struct {
	Dirs map[string]*Jump `json:"dirs"`
}

// Jump is one folder's rank and last visit.
type Jump struct {
	Rank float64   `json:"rank"`
	Last time.Time `json:"last"`
}

// maxRank is the total over which ranks age (zoxide's _ZO_MAXAGE).
const maxRank = 10000

// Visit records a visit to dir.
func (j *Jumps) Visit(dir string, now time.Time) {
	if j.Dirs == nil {
		j.Dirs = map[string]*Jump{}
	}
	d := j.Dirs[dir]
	if d == nil {
		d = &Jump{}
		j.Dirs[dir] = d
	}
	d.Rank++
	d.Last = now
	total := 0.0
	for _, d := range j.Dirs {
		total += d.Rank
	}
	if total > maxRank {
		for k, d := range j.Dirs {
			d.Rank *= 0.9 * maxRank / total
			if d.Rank < 1 {
				delete(j.Dirs, k)
			}
		}
	}
}

// Forget removes a folder that no longer exists.
func (j *Jumps) Forget(dir string) { delete(j.Dirs, dir) }

// frecency weighs a rank by how recent the last visit was, as zoxide.
func frecency(d *Jump, now time.Time) float64 {
	switch age := now.Sub(d.Last); {
	case age < time.Hour:
		return d.Rank * 4
	case age < 24*time.Hour:
		return d.Rank * 2
	case age < 7*24*time.Hour:
		return d.Rank / 2
	default:
		return d.Rank / 4
	}
}

// Find returns the folders matching query, best first: the match on the
// folder's own name counts most, then the whole path, then frecency.
func (j *Jumps) Find(query string, now time.Time, limit int) []string {
	q := fold(strings.TrimSpace(query))
	type hit struct {
		dir   string
		score float64
	}
	var hits []hit
	for dir, d := range j.Dirs {
		f := frecency(d, now)
		if q == "" {
			hits = append(hits, hit{dir, f})
			continue
		}
		folded := fold(dir)
		s := Score(q, fold(filepath.Base(dir)))
		if s > 0 {
			s += 1 << 21
		} else if s = Score(q, folded); s == 0 {
			continue
		}
		hits = append(hits, hit{dir, float64(s) + math.Log1p(f)*1000})
	}
	slices.SortFunc(hits, func(a, b hit) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.dir, b.dir)
	})
	out := make([]string, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.dir)
	}
	return out
}
