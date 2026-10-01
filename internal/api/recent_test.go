package api

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/index"
)

func feed(c *collapser, fs []index.File) int {
	for i, f := range fs {
		if !c.add(f) {
			return i
		}
	}
	return len(fs)
}

func TestCollapseBulkWrites(t *testing.T) {
	const minute = 60 * 1000
	var fs []index.File
	add := func(p string, at int64) { fs = append(fs, index.File{Vol: "v", Path: p, Size: 1, Mtime: at}) }
	add("docs/new.txt", 100*minute)
	for i := range 80 {
		add(fmt.Sprintf("dl/big/f%02d.log", i), 90*minute-int64(i))
	}
	add("docs/a.txt", 89*minute)
	for i := range 5 {
		add(fmt.Sprintf("pics/p%d.jpg", i), 80*minute-int64(i))
	}
	for i := range 7 {
		add(fmt.Sprintf("dl/big/g%d.log", i), 50*minute-int64(i))
	}
	add("root.txt", 10*minute)
	c := newCollapser(200, 0)
	feed(c, fs)
	out, runs := c.result(false)
	if len(runs) != 2 || runs[0].Count != 80 || runs[0].Dir != "dl/big" || runs[0].Mtime != 90*minute || runs[1].Count != 7 || runs[1].More {
		t.Fatalf("runs %+v", runs)
	}
	plain, grouped := 0, map[int]int{}
	for _, f := range out {
		if f.Run == nil {
			plain++
		} else {
			grouped[*f.Run]++
		}
	}
	if plain != 8 || grouped[0] != runMembers || grouped[1] != 7 || out[len(out)-1].Path != "root.txt" {
		t.Fatalf("plain %d grouped %v last %+v", plain, grouped, out[len(out)-1])
	}

	c = newCollapser(200, 0)
	feed(c, fs)
	if _, runs = c.result(true); !runs[1].More || runs[0].More {
		t.Fatalf("a capped scan marks only the runs still open: %+v", runs)
	}

	c = newCollapser(3, 0)
	if n := feed(c, fs); n != 87 {
		t.Fatalf("stopped after %d rows, want 87", n)
	}
	out, runs = c.result(false)
	if len(runs) != 1 || runs[0].Count != 80 || len(out) != 2+runMembers {
		t.Fatalf("limited: %+v, %d files", runs, len(out))
	}
}

func TestRecentSeesPastAHugeBurst(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tx, _ := d.Begin()
	for i := range 25000 {
		tx.Exec(`INSERT INTO files (vol, path, dir, size, mtime) VALUES ('v', ?, 0, 1, ?)`, fmt.Sprintf("sync/f%05d", i), int64(10_000_000-i))
	}
	tx.Exec(`INSERT INTO files (vol, path, dir, size, mtime) VALUES ('v', 'docs/mine.txt', 0, 1, 1000)`)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	out, runs, err := recentRuns(context.Background(), index.New(d), 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Count != 25000 || runs[0].More || out[len(out)-1].Path != "docs/mine.txt" {
		t.Fatalf("runs %+v, last %+v", runs, out[len(out)-1])
	}
}

func TestRunsEndAtLocalMidnight(t *testing.T) {
	const day = 24 * 60 * 60 * 1000
	tz := int64(8 * 60 * 60 * 1000)
	var fs []index.File
	for i := range 2 * 24 * 60 / 3 {
		fs = append(fs, index.File{Vol: "v", Path: fmt.Sprintf("logs/%05d", i), Mtime: 100*day - tz - 60*1000 - int64(i)*3*60*1000})
	}
	c := newCollapser(200, tz)
	feed(c, fs)
	_, runs := c.result(false)
	if len(runs) != 2 || runs[0].Count != 480 || runs[1].Count != 480 {
		t.Fatalf("periodic writes should split per local day: %d runs %+v", len(runs), runs[:min(len(runs), 3)])
	}
}
