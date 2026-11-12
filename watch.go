package main

import (
	"sort"
	"sync"
	"time"
)

// watcher polls the dirs for new or modified files and queues them in the order
// they changed. Polling (vs fsnotify) keeps it to zero file handles, which matters
// on macOS where kqueue needs one per file.
type watcher struct {
	dirs []string
	opts ScanOpts
	want map[string]bool

	seen map[string]SourceFile // goroutine-only
	gone map[string]int        // polls each seen file has been missing for; goroutine-only

	mu    sync.Mutex
	queue []watchItem
}

type watchItem struct {
	f     SourceFile
	isNew bool
}

func newWatcher(dirs []string, opts ScanOpts, want map[string]bool, initial []SourceFile) *watcher {
	w := &watcher{dirs: dirs, opts: opts, want: want, seen: map[string]SourceFile{}, gone: map[string]int{}}
	for _, f := range initial {
		w.seen[f.Abs] = f
	}
	return w
}

const goneAfter = 3

func (w *watcher) run(every time.Duration) {
	for range time.Tick(every) {
		w.poll()
	}
}

func (w *watcher) poll() {
	var changed []watchItem
	current := map[string]bool{}
	for _, f := range scan(w.dirs, w.opts) {
		if !w.want[f.Ext] {
			continue
		}
		current[f.Abs] = true
		prev, ok := w.seen[f.Abs]
		if !ok || prev.ModTime != f.ModTime || prev.Size != f.Size {
			changed = append(changed, watchItem{f, !ok})
		}
		w.seen[f.Abs] = f
	}
	// Forget deleted files so re-creating one counts as new. Give it a couple of
	// polls first though: a save can leave the file missing or empty for a moment
	// (truncate then write, or delete then rename), and that's not a new file.
	for p := range w.seen {
		if current[p] {
			delete(w.gone, p)
		} else if w.gone[p]++; w.gone[p] >= goneAfter {
			delete(w.seen, p)
			delete(w.gone, p)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].f.ModTime < changed[j].f.ModTime })

	w.mu.Lock()
	defer w.mu.Unlock()
next:
	for _, c := range changed {
		for i := range w.queue { // already waiting: refresh it but keep its place in line
			if w.queue[i].f.Abs == c.f.Abs {
				w.queue[i].f = c.f
				continue next
			}
		}
		w.queue = append(w.queue, c)
	}
}

func (w *watcher) pop() (watchItem, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.queue) == 0 {
		return watchItem{}, false
	}
	it := w.queue[0]
	w.queue = w.queue[1:]
	return it, true
}

func (w *watcher) pending() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.queue)
}
