package httpx

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
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

func Tagged(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		Error(w, err)
		return
	}
	sum := sha256.Sum256(b)
	tag := `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`
	if Fresh(w, r, tag) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func Fresh(w http.ResponseWriter, r *http.Request, tag string) bool {
	h := w.Header()
	h.Set("ETag", tag)
	h.Set("Cache-Control", "private, no-cache")
	tag = strings.TrimPrefix(tag, "W/")
	for c := range strings.SplitSeq(r.Header.Get("If-None-Match"), ",") {
		if c = strings.TrimPrefix(strings.TrimSpace(c), "W/"); c == tag || c == "*" {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}

func Fail(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

func Error(w http.ResponseWriter, err error) {
	status, msg := Status(err)
	Fail(w, status, msg)
}

func Status(err error) (int, string) {
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, db.ErrNotFound):
		return 404, "not found"
	case errors.Is(err, fs.ErrExist) || errors.Is(err, db.ErrConflict):
		return 409, "already exists"
	case errors.Is(err, vol.ErrBadPath):
		return 400, "bad path"
	case errors.Is(err, fs.ErrPermission):
		return 403, "forbidden"
	case NoSpace(err):
		return 507, "insufficient storage"
	}
	slog.Error("request failed", "err", err)
	return 500, "internal error"
}

func NoSpace(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}

func Read(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
