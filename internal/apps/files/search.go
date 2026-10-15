package files

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Hit is an entry found by Search.
type Hit struct {
	Dir   string // the folder holding it
	Entry Entry
	Score int
}

// Path is where the hit is.
func (h Hit) Path() string { return filepath.Join(h.Dir, h.Entry.Name) }

// SearchOptions shape a search.
type SearchOptions struct {
	Hidden bool // look in hidden folders and at hidden files too
	Limit  int  // stop after this many hits; 0 is 10 000
}

// skipDirs are folders searching never goes into: build output and
// caches with many files and nothing one looks for by name.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".cache": true, "__pycache__": true,
	".venv": true, ".tox": true, ".hg": true, ".svn": true,
}

// Search walks below root for names matching query (fuzzy, as Score),
// on several goroutines, and sends hits in batches to found as they come
// (found is called from one goroutine at a time). It returns when the
// walk is done, ctx is cancelled or the limit is reached. Links to
// folders are not followed, so it cannot loop.
func Search(ctx context.Context, root, query string, o SearchOptions, found func([]Hit)) {
	q := fold(strings.TrimSpace(query))
	if q == "" {
		return
	}
	limit := o.Limit
	if limit <= 0 {
		limit = 10000
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu    sync.Mutex
		total int
		wg    sync.WaitGroup
	)
	dirs := make(chan string, 4096)
	var pending sync.WaitGroup // folders queued or being read
	emit := func(hs []Hit) {
		if len(hs) == 0 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if total >= limit {
			return
		}
		if total+len(hs) > limit {
			hs = hs[:limit-total]
		}
		total += len(hs)
		found(hs)
		if total >= limit {
			cancel()
		}
	}
	var queue func(string)
	visit := func(dir string) {
		defer pending.Done()
		if ctx.Err() != nil {
			return
		}
		f, err := os.Open(dir)
		if err != nil {
			return
		}
		ds, _ := f.ReadDir(-1)
		f.Close()
		var hs []Hit
		for _, d := range ds {
			name := d.Name()
			hidden := strings.HasPrefix(name, ".")
			if hidden && !o.Hidden {
				continue
			}
			e := newEntry(name)
			if s := Score(q, e.key); s > 0 {
				if fi, err := d.Info(); err == nil {
					fill(&e, filepath.Join(dir, name), fi)
				}
				hs = append(hs, Hit{Dir: dir, Entry: e, Score: s})
			}
			if d.IsDir() && !skipDirs[name] {
				queue(filepath.Join(dir, name))
			}
		}
		emit(hs)
	}
	queue = func(dir string) {
		pending.Add(1)
		select {
		case dirs <- dir:
		default:
			visit(dir) // the queue is full: read it here, depth first
		}
	}
	for range min(runtime.GOMAXPROCS(0), 8) {
		wg.Go(func() {
			for d := range dirs {
				visit(d)
			}
		})
	}
	queue(root)
	pending.Wait()
	close(dirs)
	wg.Wait()
}
