package api

import (
	"slices"
	"testing"

	"github.com/m1ngsama/filebox/internal/vol"
)

func TestFilesystemsGroupSubvolumesByNumbers(t *testing.T) {
	pool := vol.Usage{Used: 40, Free: 60, Total: 100, Type: 0x9123683e}
	other := vol.Usage{Used: 40, Free: 60, Total: 100, Type: 0xef53}
	got := filesystems([]string{"a", "b", "c", "a", "d"}, []vol.Usage{pool, pool, other, {Used: 41, Free: 59, Total: 100}, {}})
	if want := []string{"a", "a", "c", "a", "d"}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
