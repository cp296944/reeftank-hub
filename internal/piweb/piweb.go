// Package piweb is pi-bridge's UX layer over the unmodified upstream web UI.
//
// It is a single HTTP middleware wrapping httpapi's handler. It never touches
// the vendored shared-ui files; instead it:
//
//   - injects <script src="/pi/overlay.js"> into served HTML (zh-Hant strings,
//     an "updates" widget, a version footer)
//   - serves /pi/* (the overlay script + its dictionary)
//   - intercepts /api/profiles and /api/profiles/<name> and stores profiles
//     per-lamp on disk, so an OTA update or a lamp swap never disturbs them
//   - rewrites POST /api/push bodies to honour schedule_shift_minutes (upstream
//     pc-bridge ignores it)
package piweb

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cp296944/reeftank-hub/internal/profiles"
)

//go:embed assets/overlay.js assets/dict-zh-Hant.json
var assets embed.FS

type Deps struct {
	Next       http.Handler
	Profiles   *profiles.Store
	Version    string
	UpdateRepo string
}

type handler struct {
	Deps
	overlayJS []byte
	dict      []byte
}

func Wrap(d Deps) http.Handler {
	oj, _ := assets.ReadFile("assets/overlay.js")
	dj, _ := assets.ReadFile("assets/dict-zh-Hant.json")
	return &handler{Deps: d, overlayJS: oj, dict: dj}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/pi/overlay.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(h.overlayJS)
		return
	case r.URL.Path == "/pi/dict-zh-Hant.json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(h.dict)
		return
	case r.URL.Path == "/pi/info":
		writeJSON(w, map[string]any{"version": h.Version, "update_repo": h.UpdateRepo})
		return
	case h.Profiles != nil && (r.URL.Path == "/api/profiles" || strings.HasPrefix(r.URL.Path, "/api/profiles/")):
		h.Profiles.ServeHTTP(w, r, h.lampID(r.Context()))
		return
	case r.Method == http.MethodPost && r.URL.Path == "/api/push":
		h.rewritePush(r)
	}

	// HTML responses get the overlay <script> injected.
	if wantsHTML(r) {
		rec := &htmlInjector{ResponseWriter: w}
		h.Next.ServeHTTP(rec, r)
		rec.flush()
		return
	}
	h.Next.ServeHTTP(w, r)
}

func (h *handler) lampID(ctx context.Context) string {
	if h.Profiles == nil {
		return "default"
	}
	return h.Profiles.LampID(ctx)
}

// rewritePush rotates the 24 schedule rows by schedule_shift_minutes before the
// vendored handler sees them, then strips the field.
func (h *handler) rewritePush(r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return
	}
	shiftMin := 0
	if raw, ok := m["schedule_shift_minutes"]; ok {
		_ = json.Unmarshal(raw, &shiftMin)
	}
	steps := ((shiftMin/60)%24 + 24) % 24
	if steps != 0 {
		var sched [][]int
		if raw, ok := m["schedule"]; ok && json.Unmarshal(raw, &sched) == nil && len(sched) == 24 {
			rotated := make([][]int, 24)
			for i := 0; i < 24; i++ {
				src := append([]int(nil), sched[(i-steps+24)%24]...)
				if len(src) > 0 {
					src[0] = i // keep the hour index in column 0
				}
				rotated[i] = src
			}
			if nb, err := json.Marshal(rotated); err == nil {
				m["schedule"] = nb
			}
		}
	}
	delete(m, "schedule_shift_minutes")
	if nb, err := json.Marshal(m); err == nil {
		body = nb
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	if steps != 0 {
		slog.Debug("piweb: applied schedule shift", "hours", steps)
	}
}

func wantsHTML(r *http.Request) bool {
	p := r.URL.Path
	if p == "/" || p == "/static/" || strings.HasSuffix(p, ".html") {
		return true
	}
	return false
}

// htmlInjector buffers the response and, if it's HTML, inserts the overlay
// <script> just before </head> (or </body>, or appends).
type htmlInjector struct {
	http.ResponseWriter
	buf     bytes.Buffer
	status  int
	isHTML  bool
	sniffed bool
}

func (h *htmlInjector) WriteHeader(code int) {
	h.status = code
	ct := h.Header().Get("Content-Type")
	h.isHTML = strings.Contains(ct, "text/html")
	h.sniffed = true
}

func (h *htmlInjector) Write(p []byte) (int, error) {
	if !h.sniffed {
		h.WriteHeader(http.StatusOK)
	}
	return h.buf.Write(p)
}

func (h *htmlInjector) flush() {
	body := h.buf.Bytes()
	if h.status == 0 {
		h.status = http.StatusOK
	}
	if h.isHTML || bytes.Contains(bytes.ToLower(body[:min(512, len(body))]), []byte("<!doctype html")) {
		tag := []byte(`<script src="/pi/overlay.js" defer></script>`)
		lower := bytes.ToLower(body)
		if i := bytes.Index(lower, []byte("</head>")); i >= 0 {
			body = append(body[:i], append(tag, body[i:]...)...)
		} else if i := bytes.Index(lower, []byte("</body>")); i >= 0 {
			body = append(body[:i], append(tag, body[i:]...)...)
		} else {
			body = append(body, tag...)
		}
		h.Header().Del("Content-Length")
	}
	h.ResponseWriter.WriteHeader(h.status)
	_, _ = h.ResponseWriter.Write(body)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
