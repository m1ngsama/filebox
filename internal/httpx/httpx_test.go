package httpx

import (
	"fmt"
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/vol"
)

func TestErrorMapping(t *testing.T) {
	cases := map[error]int{
		fs.ErrNotExist:                   404,
		fmt.Errorf("x: %w", fs.ErrExist): 409,
		vol.ErrBadPath:                   400,
		fs.ErrPermission:                 403,
		ErrNoSpace:                       507,
		fmt.Errorf("disk on fire"):       500,
	}
	for err, want := range cases {
		w := httptest.NewRecorder()
		Error(w, err)
		if w.Code != want {
			t.Errorf("%v → %d, want %d", err, w.Code, want)
		}
		if !strings.Contains(w.Body.String(), `"error"`) {
			t.Errorf("body %q", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	Error(w, fmt.Errorf("secret detail /mnt/x"))
	if strings.Contains(w.Body.String(), "/mnt") {
		t.Error("500 leaked internal detail")
	}
}

func TestReadRejectsUnknownFields(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"a":1,"b":2}`))
	var v struct{ A int }
	if err := Read(r, &v); err == nil {
		t.Fatal("want error")
	}
}
