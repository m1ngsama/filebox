// Package notify tells outside services about what happens in filebox, through webhooks the user has added.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/vol"
)

const EventDiskLow = "disk_low"

// Kinds are the events a webhook can ask for.
var Kinds = []string{db.EventUpload, db.EventDownload, db.EventLogin, db.EventLoginFailed, db.EventShareCreate, db.EventTokenCreate, EventDiskLow}

const (
	queueSize = 256
	perMinute = 30
	maxHooks  = 10
	timeout   = 10 * time.Second
)

var Formats = []string{"json", "text"}

var retries = []time.Duration{2 * time.Second, 10 * time.Second}

type Notifier struct {
	DB     *db.DB
	Client *http.Client

	queue chan job
	mu    sync.Mutex
	seen  map[string]int64
	sent  map[int64][]int64
}

type job struct {
	hook db.Webhook
	body []byte
	text string
	done chan error
}

func New(d *db.DB) *Notifier {
	return &Notifier{
		DB:     d,
		Client: &http.Client{Timeout: timeout},
		queue:  make(chan job, queueSize),
		seen:   map[string]int64{},
		sent:   map[int64][]int64{},
	}
}

func MaxHooks() int { return maxHooks }

func ValidURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(raw) <= 2048
}

type payload struct {
	Event string `json:"event"`
	At    int64  `json:"at"`
	Text  string `json:"text"`
	Name  string `json:"name,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Vol   string `json:"vol,omitempty"`
	Path  string `json:"path,omitempty"`
	Share string `json:"share,omitempty"`
}

// Event queues e for every webhook that asked for its kind. It never blocks the caller.
func (n *Notifier) Event(e db.Event) {
	if !slices.Contains(Kinds, e.Kind) {
		return
	}
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	if !n.fresh(e) {
		return
	}
	hooks, err := n.DB.Webhooks(0)
	if err != nil {
		slog.Warn("webhooks", "err", err)
		return
	}
	for _, h := range hooks {
		if !slices.Contains(h.Kinds, e.Kind) || (e.UserID != 0 && h.UserID != e.UserID) || !n.allow(h.ID, e.At) {
			continue
		}
		p := payload{Event: e.Kind, At: e.At, Text: Text(h.Lang, e), Name: e.Name, Size: e.Size, Vol: e.Vol, Path: e.Path, Share: e.Target}
		body, _ := json.Marshal(p)
		n.enqueue(job{hook: h, body: body, text: p.Text})
	}
}

func (n *Notifier) enqueue(j job) {
	select {
	case n.queue <- j:
	default:
		slog.Warn("webhook queue full, notification dropped", "hook", j.hook.ID)
	}
}

// fresh drops a repeat of the same download within the hour, which the activity log merges too.
func (n *Notifier) fresh(e db.Event) bool {
	if e.Kind != db.EventDownload {
		return true
	}
	k := strconv.FormatInt(e.ShareID, 10) + "|" + e.Visitor + "|" + e.Name
	n.mu.Lock()
	defer n.mu.Unlock()
	if at, ok := n.seen[k]; ok && e.At-at < 3600 {
		return false
	}
	if len(n.seen) > 1<<14 {
		clear(n.seen)
	}
	n.seen[k] = e.At
	return true
}

// allow caps what one webhook receives, so a burst of uploads cannot flood a chat.
func (n *Notifier) allow(id, now int64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	recent := slices.DeleteFunc(n.sent[id], func(t int64) bool { return now-t >= 60 })
	if len(recent) >= perMinute {
		n.sent[id] = recent
		return false
	}
	n.sent[id] = append(recent, now)
	return true
}

// Run delivers queued notifications until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-n.queue:
			go n.deliver(ctx, j)
		}
	}
}

func (n *Notifier) deliver(ctx context.Context, j job) {
	var err error
	for i := 0; ; i++ {
		if err = n.send(ctx, j.hook, j.body, j.text); err == nil || i >= len(retries) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retries[i]):
		}
	}
	n.record(j.hook.ID, err)
}

func (n *Notifier) record(id int64, err error) {
	status := "ok"
	if err != nil {
		status = err.Error()
		slog.Warn("webhook failed", "hook", id, "err", err)
	}
	n.DB.WebhookResult(id, time.Now().Unix(), status)
}

// Test sends a sample notification and reports whether the receiver took it.
func (n *Notifier) Test(ctx context.Context, h db.Webhook) error {
	e := db.Event{Kind: "test", At: time.Now().Unix()}
	p := payload{Event: "test", At: e.At, Text: Text(h.Lang, e)}
	body, _ := json.Marshal(p)
	err := n.send(ctx, h, body, p.Text)
	n.record(h.ID, err)
	return err
}

func (n *Notifier) send(ctx context.Context, h db.Webhook, body []byte, text string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ct := "application/json"
	if h.Format == "text" {
		body, ct = []byte(text), "text/plain; charset=utf-8"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", h.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ct)
	req.Header.Set("User-Agent", "filebox")
	req.Header.Set("Title", "filebox")
	if h.Secret != "" {
		m := hmac.New(sha256.New, []byte(h.Secret))
		m.Write(body)
		req.Header.Set("X-Filebox-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	res, err := n.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%s", unwrap(err))
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return nil
}

func unwrap(err error) string {
	if u, ok := err.(*url.Error); ok {
		err = u.Err
	}
	return err.Error()
}

// Watch warns once when a volume drops below 10% free, and again only after it has recovered past 15%.
func (n *Notifier) Watch(ctx context.Context, vols *vol.Set, every time.Duration) {
	low := map[string]bool{}
	for {
		for _, v := range vols.All() {
			u, err := v.Usage()
			if err != nil || u.Total == 0 {
				continue
			}
			pct := float64(u.Free) / float64(u.Total)
			switch {
			case pct < 0.10 && !low[v.Name]:
				low[v.Name] = true
				n.Event(db.Event{Kind: EventDiskLow, Vol: v.Name, Size: int64(u.Free)})
			case pct > 0.15:
				delete(low, v.Name)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
