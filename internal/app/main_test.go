package app

import (
	"github.com/m1ngsama/filebox/internal/render"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	render.RunWorker()
	os.Exit(m.Run())
}
