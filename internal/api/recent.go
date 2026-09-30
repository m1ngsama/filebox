package api

import (
	"path"

	"github.com/m1ngsama/filebox/internal/index"
)

const (
	runGap     = 10 * 60 * 1000
	runMin     = 6
	runMembers = 50
	recentScan = 20000
)

type recentFile struct {
	index.File
	Run *int `json:"run,omitempty"`
}

type recentRun struct {
	Vol   string `json:"vol"`
	Dir   string `json:"dir"`
	Count int    `json:"count"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

func collapse(fs []index.File, limit int) ([]recentFile, []recentRun) {
	type open struct {
		run  int
		last int64
	}
	var runs []recentRun
	of := make([]int, len(fs))
	cur := map[[2]string]*open{}
	for i, f := range fs {
		dir := path.Dir(f.Path)
		if dir == "." {
			dir = ""
		}
		k := [2]string{f.Vol, dir}
		o := cur[k]
		if o == nil || o.last-f.Mtime > runGap {
			runs = append(runs, recentRun{Vol: f.Vol, Dir: dir, Mtime: f.Mtime})
			o = &open{run: len(runs) - 1}
			cur[k] = o
		}
		o.last = f.Mtime
		runs[o.run].Count++
		runs[o.run].Size += f.Size
		of[i] = o.run
	}
	out, kept := []recentFile{}, []recentRun{}
	at, members := map[int]int{}, map[int]int{}
	shown := 0
	for i, f := range fs {
		r := of[i]
		if runs[r].Count < runMin {
			if shown < limit {
				out = append(out, recentFile{File: f})
				shown++
			}
			continue
		}
		j, ok := at[r]
		if !ok {
			if shown >= limit {
				continue
			}
			j = len(kept)
			kept = append(kept, runs[r])
			at[r] = j
			shown++
		}
		if members[r] < runMembers {
			members[r]++
			out = append(out, recentFile{File: f, Run: &j})
		}
	}
	return out, kept
}
