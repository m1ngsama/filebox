package render

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	RunWorker()
	os.Exit(m.Run())
}
