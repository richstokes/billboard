package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherQueue(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond) // keep mod times distinct
	}
	write("a.go", "package a\n")
	write("notes.txt", "ignored\n")

	want := map[string]bool{".go": true}
	w := newWatcher([]string{dir}, ScanOpts{}, want, scan([]string{dir}, ScanOpts{}))
	w.poll()
	if n := w.pending(); n != 0 {
		t.Fatalf("nothing changed yet, got %d queued", n)
	}

	write("b.go", "package b\n")
	write("a.go", "package a\n\nfunc A() {}\n")
	write("notes.txt", "still ignored\n")
	w.poll()
	write("b.go", "package b\n\nfunc B() {}\n") // changed again while queued
	w.poll()

	if n := w.pending(); n != 2 {
		t.Fatalf("want 2 queued, got %d", n)
	}
	first, _ := w.pop()
	second, _ := w.pop()
	if filepath.Base(first.f.Abs) != "b.go" || !first.isNew {
		t.Errorf("first should be new b.go, got %s new=%v", first.f.Abs, first.isNew)
	}
	if filepath.Base(second.f.Abs) != "a.go" || second.isNew {
		t.Errorf("second should be modified a.go, got %s new=%v", second.f.Abs, second.isNew)
	}
	if _, ok := w.pop(); ok {
		t.Error("queue should be empty")
	}
}

// A save that briefly empties the file (truncate, then write) shouldn't make it
// look like a brand new file, but one that's been gone a while should.
func TestWatcherSaveIsNotNew(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package a\n")
	want := map[string]bool{".go": true}
	w := newWatcher([]string{dir}, ScanOpts{}, want, scan([]string{dir}, ScanOpts{}))

	write("") // caught mid-save: empty files aren't scanned
	w.poll()
	write("package a\n\nfunc A() {}\n")
	w.poll()
	if it, ok := w.pop(); !ok || it.isNew {
		t.Fatalf("want a.go queued as modified, got ok=%v new=%v", ok, it.isNew)
	}

	os.Remove(p)
	for i := 0; i < goneAfter; i++ {
		w.poll()
	}
	write("package a\n")
	w.poll()
	if it, ok := w.pop(); !ok || !it.isNew {
		t.Fatalf("want re-created a.go queued as new, got ok=%v new=%v", ok, it.isNew)
	}
}
