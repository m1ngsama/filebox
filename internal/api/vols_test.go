package api

import (
	"slices"
	"testing"

	"github.com/m1ngsama/filebox/internal/vol"
)

func TestFilesystemsGroupBtrfsSubvolumesBySize(t *testing.T) {
	pool := vol.Usage{Used: 40, Free: 60, Total: 100, Type: btrfs, Size: 1000}
	busy := vol.Usage{Used: 41, Free: 59, Total: 100, Type: btrfs, Size: 1000}
	ext4 := vol.Usage{Used: 40, Free: 60, Total: 100, Type: 0xef53, Size: 1000}
	got := filesystems([]string{"a", "b", "c", "d", "a", "e"}, []vol.Usage{pool, busy, ext4, ext4, {}, {Type: btrfs, Size: 2000}})
	if want := []string{"a", "a", "c", "d", "a", "e"}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
