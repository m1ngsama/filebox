package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/vol"
)

func TestTrashPurgeNeverEatsARestore(t *testing.T) {
	f := newTestApp(t)
	v, _ := f.App.Vols.Get("v")
	const files = 1500
	for round := range 8 {
		name := fmt.Sprintf("d%d", round)
		for i := range files {
			f.write(t, fmt.Sprintf("%s/f%03d", name, i), "x")
		}
		w := f.do("POST", "/api/rm", body(`{"vol":"v","paths":["`+name+`"]}`))
		id := decode[struct{ Trashed []struct{ ID string } }](t, w).Trashed[0].ID
		var restored int
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(round) * time.Millisecond)
			restored = f.do("POST", "/api/trash/restore", body(`{"vol":"v","id":"`+id+`"}`)).Code
		}()
		go func() {
			defer wg.Done()
			if round%2 == 0 {
				v.PurgeTrash(id)
			} else {
				f.do("POST", "/api/trash/delete", body(`{"vol":"v","ids":["`+id+`"]}`))
			}
		}()
		wg.Wait()
		des, err := os.ReadDir(filepath.Join(f.Dir, name))
		switch {
		case restored == 204 && (err != nil || len(des) != files):
			t.Fatalf("round %d: restore returned 204 but the folder has %d files (%v)", round, len(des), err)
		case restored != 204 && err == nil:
			t.Fatalf("round %d: restore failed with %d but the folder is live", round, restored)
		}
	}
	claim := ".purge-1790000000000-0123abcd"
	os.MkdirAll(filepath.Join(f.Dir, vol.TrashDir, claim, "a"), 0o755)
	os.MkdirAll(filepath.Join(f.Dir, vol.TrashDir, ".purge-mine", "a"), 0o755)
	w := f.do("GET", "/api/trash?vol=v", nil)
	for _, it := range decode[struct{ Items []struct{ ID string } }](t, w).Items {
		if vol.Purging(it.ID) {
			t.Fatal("claimed entry listed")
		}
	}
	v.SweepPurges()
	if _, err := os.Stat(filepath.Join(f.Dir, vol.TrashDir, claim)); err == nil {
		t.Fatal("leftover claim kept")
	}
	if _, err := os.Stat(filepath.Join(f.Dir, vol.TrashDir, ".purge-mine", "a")); err != nil {
		t.Fatal("a folder filebox did not claim was swept")
	}
}
