// Package hubweb serves the ReefTank Hub shell while preserving the complete
// legacy K7 handler and API surface underneath it.
package hubweb

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/cp296944/reeftank-hub/internal/bootstrap"
)

//go:embed assets/*
var assetFiles embed.FS

type Options struct {
	K7          http.Handler
	Version     string
	InstallRoot string
}

type handler struct {
	Options
	assets http.Handler
}

func New(o Options) http.Handler {
	sub, err := fs.Sub(assetFiles, "assets")
	if err != nil {
		panic(err)
	}
	return &handler{Options: o, assets: http.FileServer(http.FS(sub))}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		h.serveIndex(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/hub/status":
		h.serveStatus(w)
	case r.Method == http.MethodGet && r.URL.Path == "/api/bootstrap/status":
		writeJSON(w, bootstrap.Inspect(h.InstallRoot))
	case r.URL.Path == "/K7":
		http.Redirect(w, r, "/K7/", http.StatusPermanentRedirect)
	case strings.HasPrefix(r.URL.Path, "/K7/"):
		h.serveK7(w, r)
	case r.Method == http.MethodGet && isModulePath(r.URL.Path):
		h.serveModule(w, r)
	case strings.HasPrefix(r.URL.Path, "/hub-assets/"):
		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/hub-assets")
		h.assets.ServeHTTP(w, r2)
	default:
		h.K7.ServeHTTP(w, r)
	}
}

func isModulePath(p string) bool {
	switch p {
	case "/dosing", "/dosing/", "/power", "/power/", "/water", "/water/", "/system", "/system/":
		return true
	default:
		return false
	}
}

func (h *handler) serveModule(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusPermanentRedirect)
		return
	}
	name := strings.Trim(path.Clean(r.URL.Path), "/")
	b, err := assetFiles.ReadFile("assets/module.html")
	if err != nil {
		http.Error(w, "hub module UI unavailable", http.StatusInternalServerError)
		return
	}
	replacements := map[string][3]string{
		"dosing": {"魔點四頭滴定", "校正、容器、排程與手動滴定", "軟體架構建構中；實機 BLE 驗證將在後續進行。"},
		"power":  {"電源監控", "三組排插與 18 路設備", "等待 Home Assistant WebSocket 整合。"},
		"water":  {"水質與換水", "Google 試算表為主資料來源", "等待水質鏡射及本機歷史資料庫。"},
		"system": {"系統狀態", "連線、版本、備份與 OTA", "正在建立安全的 bootstrap 與維護流程。"},
	}
	copyText := string(b)
	info := replacements[name]
	copyText = strings.ReplaceAll(copyText, "{{TITLE}}", info[0])
	copyText = strings.ReplaceAll(copyText, "{{SUBTITLE}}", info[1])
	copyText = strings.ReplaceAll(copyText, "{{DETAIL}}", info[2])
	copyText = strings.ReplaceAll(copyText, "{{MODULE}}", name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(copyText))
}

func (h *handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	b, err := assetFiles.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "hub UI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

// The embedded K7 UI is still rooted at /static and uses the existing /api
// endpoints. Rewriting only the initial document lets /K7/ become its stable
// human-facing entry point without forking that upstream UI.
func (h *handler) serveK7(w http.ResponseWriter, r *http.Request) {
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/static/" + strings.TrimPrefix(r.URL.Path, "/K7/")
	h.K7.ServeHTTP(w, r2)
}

func (h *handler) serveStatus(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"name":    "ReefTank Hub",
		"version": h.Version,
		"modules": map[string]string{
			"k7":             "available",
			"home_assistant": "mapping_ready",
			"water_quality":  "planned",
			"temperature":    "planned",
			"dosing":         "simulation_planned",
		},
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
