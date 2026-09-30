package main

import (
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

	"github.com/m1ngsama/filebox/internal/api"
	"github.com/m1ngsama/filebox/internal/app"
	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/passkey"
	"github.com/m1ngsama/filebox/internal/render"
	"github.com/m1ngsama/filebox/internal/thumb"
	"github.com/m1ngsama/filebox/internal/upload"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
	"github.com/m1ngsama/filebox/web"
)

const usage = `usage:
  filebox serve   -data DIR -listen ADDR -vol name=path [-vol ...] [-origin https://host ...]
  filebox passwd  -data DIR [-user NAME]      (reads the password from stdin)
  filebox token   -data DIR [-user NAME] new LABEL [-ro] | ls | rm ID
  filebox passkey -data DIR [-user NAME] ls | rm ID

-user may be omitted when the database has exactly one user; passwd creates "admin" in an empty one.`

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func main() {
	render.RunWorker()
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
	case "passkey":
		err = passkeyCmd(os.Args[2:])
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
	var vols, origins multi
	fl.Var(&vols, "vol", "volume as name=path, repeatable")
	fl.Var(&origins, "origin", "public origin that may use passkeys, e.g. https://files.example.com; repeatable")
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
	api.ClearStaging(set)
	webFS, _ := fs.Sub(web.Dist, "dist")
	up, err := upload.New(set)
	if err != nil {
		return err
	}
	au := auth.New(d)
	pk, err := passkey.New(d, au, origins)
	if err != nil {
		return err
	}
	ix := index.New(d)
	up.Index = ix
	vs := &version.Store{DB: d}
	if err := vs.Recover(set); err != nil {
		slog.Error("recover versions", "err", err)
	}
	up.Versions = vs
	go func() {
		for {
			up.Sweep(24 * time.Hour)
			d.PurgeTokens(time.Now().Unix())
			if _, err := d.PruneEvents(time.Now().Add(-90*24*time.Hour).Unix(), db.MaxEvents); err != nil {
				slog.Error("prune activity", "err", err)
			}
			pk.Sweep()
			vs.Prune(set)
			if err := ix.Scan(set); err != nil {
				slog.Error("index scan", "err", err)
			}
			time.Sleep(time.Hour)
		}
	}()
	thumbs := thumb.New(*ffmpeg, filepath.Join(*data, "thumbs"))
	thumbs.Probe(context.Background())
	a := &app.App{Vols: set, DB: d, Auth: au, Web: webFS, Uploads: up, Thumbs: thumbs, Passkeys: pk, Index: ix, Versions: vs}

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
	user := fl.String("user", "", "user name")
	fl.Parse(args)
	d, err := openDB(*data)
	if err != nil {
		return err
	}
	defer d.Close()
	name, err := pickUser(d, *user, "admin")
	if err != nil {
		return err
	}
	fmt.Fprint(os.Stderr, "new password: ")
	pw, err := readPassword(os.Stdin)
	if err != nil {
		return err
	}
	if len(pw) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	id, err := d.SetPassword(name, h)
	if err != nil {
		return err
	}
	n, err := d.DeleteTokens(id, "session")
	if err != nil {
		return err
	}
	fmt.Printf("password set, %d sessions revoked\n", n)
	return nil
}

func pickUser(d *db.DB, name, ifNone string) (string, error) {
	if name != "" {
		return name, nil
	}
	ns, err := d.UserNames()
	switch {
	case err != nil:
		return "", err
	case len(ns) == 1:
		return ns[0], nil
	case len(ns) == 0 && ifNone != "":
		return ifNone, nil
	case len(ns) == 0:
		return "", errors.New("no users yet (run filebox passwd first)")
	}
	return "", fmt.Errorf("pass -user NAME, one of: %s", strings.Join(ns, ", "))
}

func openUser(data, name string) (*db.DB, db.User, error) {
	d, err := openDB(data)
	if err != nil {
		return nil, db.User{}, err
	}
	if name, err = pickUser(d, name, ""); err == nil {
		var u db.User
		if u, err = d.UserByName(name); err == nil {
			return d, u, nil
		}
		err = fmt.Errorf("user %q: %w (run filebox passwd first)", name, err)
	}
	d.Close()
	return nil, db.User{}, err
}

func used(at int64) string {
	if at == 0 {
		return "never used"
	}
	return "last used " + time.Unix(at, 0).Format(time.DateTime)
}

func tokenCmd(args []string) error {
	fl := flag.NewFlagSet("token", flag.ExitOnError)
	data := fl.String("data", "./data", "data directory")
	user := fl.String("user", "", "user name")
	ro := fl.Bool("ro", false, "read-only token (new only)")
	fl.Parse(args)
	rest := fl.Args()
	if len(rest) == 0 {
		return errors.New(usage)
	}
	d, u, err := openUser(*data, *user)
	if err != nil {
		return err
	}
	defer d.Close()
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
			fmt.Printf("%d\t%s\t%s\t%s\n", t.ID, t.Label, t.Scope, used(t.LastUsedAt))
		}
	case "rm":
		if len(rest) < 2 {
			return errors.New("token rm needs an ID")
		}
		id, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil {
			return err
		}
		_, err = d.DeleteToken(u.ID, id, "app")
		return err
	default:
		return errors.New(usage)
	}
	return nil
}

func passkeyCmd(args []string) error {
	fl := flag.NewFlagSet("passkey", flag.ExitOnError)
	data := fl.String("data", "./data", "data directory")
	user := fl.String("user", "", "user name")
	fl.Parse(args)
	rest := fl.Args()
	if len(rest) == 0 {
		return errors.New(usage)
	}
	d, u, err := openUser(*data, *user)
	if err != nil {
		return err
	}
	defer d.Close()
	switch rest[0] {
	case "ls":
		ps, err := d.ListPasskeys(u.ID)
		if err != nil {
			return err
		}
		for _, p := range ps {
			fmt.Printf("%d\t%s\tadded %s\t%s\n", p.ID, p.Name, time.Unix(p.CreatedAt, 0).Format(time.DateTime), used(p.LastUsedAt))
		}
	case "rm":
		if len(rest) < 2 {
			return errors.New("passkey rm needs an ID")
		}
		id, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil {
			return err
		}
		return d.DeletePasskey(u.ID, id)
	default:
		return errors.New(usage)
	}
	return nil
}
