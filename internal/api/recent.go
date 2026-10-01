package api

import (
	"path"

	"github.com/m1ngsama/filebox/internal/index"
)

const (
	runGap     = 10 * 60 * 1000
	runMin     = 6
	runMembers = 50
	recentPage = 2000
	recentCap  = 200000
)

type recentFile struct {
	index.File
	Run *int `json:"run,omitempty"`
}

type recentRun struct {
	Vol   string `json:"vol"`
	Dir   string `json:"dir"`
	Count int    `json:"count"`
	More  bool   `json:"more,omitempty"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	last  int64
}

type collapser struct {
	limit, shown, scanned int
	runs                  []recentRun
	open                  map[[2]string]int
	kept                  []index.File
	of                    []int
}

func newCollapser(limit int) *collapser {
	return &collapser{limit: limit, open: map[[2]string]int{}}
}

func weight(n int) int {
	if n >= runMin {
		return 1
	}
	return n
}

func (c *collapser) add(f index.File) bool {
	if c.shown >= c.limit {
		for k, r := range c.open {
			if c.runs[r].last-f.Mtime > runGap {
				delete(c.open, k)
			}
		}
		if len(c.open) == 0 {
			return false
		}
	}
	c.scanned++
	dir := path.Dir(f.Path)
	if dir == "." {
		dir = ""
	}
	k := [2]string{f.Vol, dir}
	r, ok := c.open[k]
	if !ok || c.runs[r].last-f.Mtime > runGap {
		if c.shown >= c.limit {
			return true
		}
		c.runs = append(c.runs, recentRun{Vol: f.Vol, Dir: dir, Mtime: f.Mtime})
		r = len(c.runs) - 1
		c.open[k] = r
	}
	run := &c.runs[r]
	c.shown += weight(run.Count+1) - weight(run.Count)
	run.Count++
	run.Size += f.Size
	run.last = f.Mtime
	if run.Count <= runMembers {
		c.kept = append(c.kept, f)
		c.of = append(c.of, r)
	}
	return true
}

func (c *collapser) result(capped bool) ([]recentFile, []recentRun) {
	if capped {
		for _, r := range c.open {
			c.runs[r].More = true
		}
	}
	out, kept := []recentFile{}, []recentRun{}
	at := map[int]int{}
	shown := 0
	for i, f := range c.kept {
		r := c.of[i]
		if c.runs[r].Count < runMin {
			if shown < c.limit {
				out = append(out, recentFile{File: f})
				shown++
			}
			continue
		}
		j, ok := at[r]
		if !ok {
			if shown >= c.limit {
				continue
			}
			j = len(kept)
			kept = append(kept, c.runs[r])
			at[r] = j
			shown++
		}
		out = append(out, recentFile{File: f, Run: &j})
	}
	return out, kept
}

func recentRuns(ix *index.Index, limit int) ([]recentFile, []recentRun, error) {
	c := newCollapser(limit)
	cur := index.Newest
	for c.scanned < recentCap {
		fs, next, err := ix.RecentFrom(cur, min(recentPage, recentCap-c.scanned))
		if err != nil {
			return nil, nil, err
		}
		for _, f := range fs {
			if !c.add(f) {
				out, runs := c.result(false)
				return out, runs, nil
			}
		}
		if len(fs) < recentPage {
			out, runs := c.result(false)
			return out, runs, nil
		}
		cur = next
	}
	out, runs := c.result(true)
	return out, runs, nil
}
