package api

import (
	"fmt"
	"testing"

	"github.com/m1ngsama/filebox/internal/index"
)

func TestCollapseBulkWrites(t *testing.T) {
	const min = 60 * 1000
	var fs []index.File
	add := func(p string, at int64) { fs = append(fs, index.File{Vol: "v", Path: p, Size: 1, Mtime: at}) }
	add("docs/new.txt", 100*min)
	for i := range 80 {
		add(fmt.Sprintf("dl/big/f%02d.log", i), 90*min-int64(i))
	}
	add("docs/a.txt", 89*min)
	for i := range 5 {
		add(fmt.Sprintf("pics/p%d.jpg", i), 80*min-int64(i))
	}
	for i := range 7 {
		add(fmt.Sprintf("dl/big/g%d.log", i), 50*min-int64(i))
	}
	add("root.txt", 10*min)
	out, runs := collapse(fs, 200)
	if len(runs) != 2 || runs[0] != (recentRun{"v", "dl/big", 80, 80, 90 * min}) || runs[1].Count != 7 || runs[1].Mtime != 50*min {
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
	if plain != 8 || grouped[0] != runMembers || grouped[1] != 7 {
		t.Fatalf("plain %d grouped %v", plain, grouped)
	}
	if out[len(out)-1].Path != "root.txt" {
		t.Fatalf("root dir: %+v", out[len(out)-1])
	}
	out, runs = collapse(fs, 3)
	if len(runs) != 1 || len(out) != 2+runMembers {
		t.Fatalf("limited: %d runs, %d files", len(runs), len(out))
	}
}
