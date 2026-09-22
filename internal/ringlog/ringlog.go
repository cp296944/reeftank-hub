// Package ringlog is a bounded in-memory log the UI can pull via /api/logs
// (the "logs" capability). It also implements slog.Handler so the daemon's
// normal logging is captured with zero extra plumbing.
package ringlog

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Entry struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Msg   string    `json:"msg"`
}

type Ring struct {
	mu   sync.Mutex
	buf  []Entry
	next int
	full bool
}

func New(capacity int) *Ring {
	if capacity < 16 {
		capacity = 16
	}
	return &Ring{buf: make([]Entry, capacity)}
}

func (r *Ring) add(e Entry) {
	r.mu.Lock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Entries returns up to `limit` most-recent entries, newest last.
func (r *Ring) Entries(limit int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.next
	total := n
	if r.full {
		total = len(r.buf)
	}
	out := make([]Entry, 0, total)
	start := 0
	if r.full {
		start = n
	}
	for i := 0; i < total; i++ {
		out = append(out, r.buf[(start+i)%len(r.buf)])
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// ---- slog.Handler ------------------------------------------------------------

type Handler struct {
	ring  *Ring
	inner slog.Handler
	attrs []slog.Attr
}

// NewHandler wraps an existing handler, mirroring every record into the ring.
func NewHandler(ring *Ring, inner slog.Handler) *Handler {
	return &Handler{ring: ring, inner: inner}
}

func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		msg += " " + a.Key + "=" + a.Value.String()
		return true
	})
	for _, a := range h.attrs {
		msg += " " + a.Key + "=" + a.Value.String()
	}
	h.ring.add(Entry{Time: r.Time, Level: r.Level.String(), Msg: msg})
	return h.inner.Handle(ctx, r)
}

func (h *Handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &Handler{ring: h.ring, inner: h.inner.WithAttrs(as), attrs: append(h.attrs, as...)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{ring: h.ring, inner: h.inner.WithGroup(name), attrs: h.attrs}
}
