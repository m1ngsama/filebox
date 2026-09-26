package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"syscall"

	"github.com/m1ngsama/filebox/internal/db"

	"github.com/m1ngsama/filebox/internal/vol"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func Fail(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

func Error(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, db.ErrNotFound):
		Fail(w, 404, "not found")
	case errors.Is(err, fs.ErrExist):
		Fail(w, 409, "already exists")
	case errors.Is(err, vol.ErrBadPath):
		Fail(w, 400, "bad path")
	case errors.Is(err, fs.ErrPermission):
		Fail(w, 403, "forbidden")
	case NoSpace(err):
		Fail(w, 507, "insufficient storage")
	default:
		slog.Error("request failed", "err", err)
		Fail(w, 500, "internal error")
	}
}

func NoSpace(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}

func Read(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
