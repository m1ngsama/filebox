package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/m1ngsama/filebox/internal/app"
	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/thumb"
	"github.com/m1ngsama/filebox/internal/upload"
	"github.com/m1ngsama/filebox/internal/vol"
	"github.com/m1ngsama/filebox/web"
)

const usage = `usage:
  filebox serve  -data DIR -listen ADDR -vol name=path [-vol ...]
  filebox passwd -data DIR [-user admin]      (reads the password from stdin)
  filebox token  -data DIR new LABEL [-ro] | ls | rm ID`

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serveCmd(os.Args[2:])
	case "passwd":
		err = passwdCmd(os.Args[2:])
	case "token":
		err = tokenCmd(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "filebox:", err)
		os.Exit(1)
	}
}

func openDB(dir string) (*db.DB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return db.Open(filepath.Join(dir, "filebox.db"))
}

func serveCmd(args []string) error {
	fl := flag.NewFlagSet("serve", flag.ExitOnError)
	data := fl.String("data", "./data", "data directory")
	listen := fl.String("listen", ":5280", "listen address")
	ffmpeg := fl.String("ffmpeg", "", "path to ffmpeg for thumbnails; empty disables them")
	var vols multi
	fl.Var(&vols, "vol", "volume as name=path, repeatable")
	fl.Parse(args)
	if len(vols) == 0 {
		return errors.New("at least one -vol is required")
	}
	d, err := openDB(*data)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Check(); err != nil {
		return err
	}
	set, err := vol.Parse(vols)
	if err != nil {
		return err
	}
	defer set.Close()
	webFS, _ := fs.Sub(web.Dist, "dist")
	up := &upload.Server{Vols: set, Dir: filepath.Join(*data, "uploads")}
	go func() {
		for {
			up.Sweep(24 * time.Hour)
			time.Sleep(time.Hour)
		}
	}()
	a := &app.App{Vols: set, DB: d, Auth: auth.New(d), Web: webFS, Uploads: up, Thumbs: thumb.New(*ffmpeg, filepath.Join(*data, "thumbs"))}

	srv := &http.Server{Addr: *listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
		close(done)
	}()
	slog.Info("filebox listening", "addr", *listen, "volumes", set.Names())
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-done
	return nil
}

func passwdCmd(args []string) error {
	fl := flag.NewFlagSet("passwd", flag.ExitOnError)
	data := fl.String("data", "./data", "data directory")
	user := fl.String("user", "admin", "user name")
	fl.Parse(args)
	fmt.Fprint(os.Stderr, "new password: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return err
	}
	pw := strings.TrimRight(line, "\r\n")
	if len(pw) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	d, err := openDB(*data)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = d.SetPassword(*user, h)
	return err
}

func tokenCmd(args []string) error {
	fl := flag.NewFlagSet("token", flag.ExitOnError)
	data := fl.String("data", "./data", "data directory")
	user := fl.String("user", "admin", "user name")
	ro := fl.Bool("ro", false, "read-only token (new only)")
	fl.Parse(args)
	rest := fl.Args()
	if len(rest) == 0 {
		return errors.New(usage)
	}
	d, err := openDB(*data)
	if err != nil {
		return err
	}
	defer d.Close()
	u, err := d.UserByName(*user)
	if err != nil {
		return fmt.Errorf("user %q: %w (run filebox passwd first)", *user, err)
	}
	switch rest[0] {
	case "new":
		if len(rest) < 2 {
			return errors.New("token new needs a LABEL")
		}
		tok, err := auth.New(d).NewAppToken(u.ID, rest[1], *ro)
		if err != nil {
			return err
		}
		fmt.Println(tok)
	case "ls":
		ts, err := d.ListTokens(u.ID, "app")
		if err != nil {
			return err
		}
		for _, t := range ts {
			fmt.Printf("%d\t%s\t%s\tlast used %s\n", t.ID, t.Label, t.Scope, time.Unix(t.LastUsedAt, 0).Format(time.DateTime))
		}
	case "rm":
		if len(rest) < 2 {
			return errors.New("token rm needs an ID")
		}
		id, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil {
			return err
		}
		return d.DeleteToken(u.ID, id)
	default:
		return errors.New(usage)
	}
	return nil
}
