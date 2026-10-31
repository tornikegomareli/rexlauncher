package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSnapshotWaitsForTheFile(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(50 * time.Millisecond)
		writeSnapshot(dir, contentFile, []pane{{Block: "a"}})
	}()
	panes, err := loadSnapshot(dir, true)
	if err != nil || len(panes) != 1 {
		t.Fatalf("%v, %+v", err, panes)
	}
}

func TestLoadSnapshotStopsWhenThePickerIsGone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snapshot")
	os.Mkdir(dir, 0o700)
	go func() {
		time.Sleep(50 * time.Millisecond)
		os.RemoveAll(dir)
	}()
	if _, err := loadSnapshot(dir, true); err == nil {
		t.Fatal("no error")
	}
}
