package index

import (
	"context"
	"crypto/sha256"
	"io"
	"path"
	"sort"
	"time"

	"github.com/m1ngsama/filebox/internal/vol"
)

type DupGroup struct {
	Size  int64  `json:"size"`
	Files []File `json:"files"`
}

type Dups struct {
	Groups  []DupGroup
	Wasted  int64
	Pending int
}

const maxDupGroups = 100

// Duplicates groups files of identical content. It only reads files that share a size with another, remembers every hash it
// computes, and stops hashing after budget so a large library is worked through over several calls; Pending counts what is left.
func (x *Index) Duplicates(ctx context.Context, vols *vol.Set, min int64, budget time.Duration) (Dups, error) {
	x.dup.Lock()
	defer x.dup.Unlock()
	d := Dups{Groups: []DupGroup{}}
	if err := x.exec(x.db, `DELETE FROM hashes WHERE NOT EXISTS (SELECT 1 FROM files f WHERE f.vol = hashes.vol AND f.path = hashes.path AND f.size = hashes.size AND f.mtime = hashes.mtime)`); err != nil {
		return d, err
	}
	rows, err := x.db.QueryContext(ctx, `SELECT f.vol, f.path, f.size, f.mtime, h.sum FROM files f
		LEFT JOIN hashes h ON h.vol = f.vol AND h.path = f.path
		WHERE f.dir = 0 AND f.size >= ?1`+visible+` AND f.size IN (SELECT size FROM files WHERE dir = 0 AND size >= ?1 GROUP BY size HAVING count(*) > 1)`, min)
	if err != nil {
		return d, err
	}
	type cand struct {
		File
		sum []byte
	}
	bySize := map[int64][]*cand{}
	for rows.Next() {
		c := &cand{}
		if err := rows.Scan(&c.Vol, &c.Path, &c.Size, &c.Mtime, &c.sum); err != nil {
			rows.Close()
			return d, err
		}
		c.Name = path.Base(c.Path)
		bySize[c.Size] = append(bySize[c.Size], c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return d, err
	}

	var todo []*cand
	for s, cs := range bySize {
		if len(cs) < 2 {
			delete(bySize, s)
			continue
		}
		for _, c := range cs {
			if c.sum == nil {
				todo = append(todo, c)
			}
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].Size > todo[j].Size })
	deadline := time.Now().Add(budget)
	var fresh []*cand
	for i, c := range todo {
		if ctx.Err() != nil || time.Now().After(deadline) {
			d.Pending = len(todo) - i
			break
		}
		if v, ok := vols.Get(c.Vol); ok {
			c.sum = hashFile(ctx, v, c.File)
		}
		if c.sum == nil {
			c.sum = []byte{}
		} else {
			fresh = append(fresh, c)
		}
	}
	if len(fresh) > 0 {
		x.w.Lock()
		tx, err := x.db.Begin()
		if err != nil {
			x.w.Unlock()
			return d, err
		}
		for _, c := range fresh {
			if _, err := tx.Exec(`INSERT INTO hashes (vol, path, size, mtime, sum) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (vol, path) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, sum = excluded.sum`, c.Vol, c.Path, c.Size, c.Mtime, c.sum); err != nil {
				tx.Rollback()
				x.w.Unlock()
				return d, err
			}
		}
		err = tx.Commit()
		x.w.Unlock()
		if err != nil {
			return d, err
		}
	}
	if err := ctx.Err(); err != nil {
		return d, err
	}

	for s, cs := range bySize {
		same := map[string][]File{}
		for _, c := range cs {
			if len(c.sum) > 0 {
				same[string(c.sum)] = append(same[string(c.sum)], c.File)
			}
		}
		for _, fs := range same {
			if len(fs) < 2 {
				continue
			}
			sort.Slice(fs, func(i, j int) bool {
				return fs[i].Mtime < fs[j].Mtime || fs[i].Mtime == fs[j].Mtime && fs[i].Vol+"/"+fs[i].Path < fs[j].Vol+"/"+fs[j].Path
			})
			d.Groups = append(d.Groups, DupGroup{Size: s, Files: fs})
			d.Wasted += s * int64(len(fs)-1)
		}
	}
	sort.Slice(d.Groups, func(i, j int) bool {
		wi, wj := d.Groups[i].Size*int64(len(d.Groups[i].Files)), d.Groups[j].Size*int64(len(d.Groups[j].Files))
		return wi > wj || wi == wj && d.Groups[i].Files[0].Path < d.Groups[j].Files[0].Path
	})
	if len(d.Groups) > maxDupGroups {
		d.Groups = d.Groups[:maxDupGroups]
	}
	return d, nil
}

// hashFile returns nil when the file cannot be read in full, or changed while it was read.
func hashFile(ctx context.Context, v *vol.Volume, f File) []byte {
	r, err := vol.Open(v.Root, f.Path)
	if err != nil {
		return nil
	}
	defer r.Close()
	h := sha256.New()
	if _, err := io.Copy(h, &ctxReader{ctx, r}); err != nil {
		return nil
	}
	if st, err := r.Stat(); err != nil || st.Size() != f.Size || st.ModTime().UnixMilli() != f.Mtime {
		return nil
	}
	return h.Sum(nil)
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
