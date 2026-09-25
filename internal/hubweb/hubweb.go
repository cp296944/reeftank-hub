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
		// Hub assets are embedded in each binary. Prevent an OTA from leaving the
		// browser on an older JavaScript UI with a newer API schema.
		w.Header().Set("Cache-Control", "no-store")
		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/hub-assets")
		h.assets.ServeHTTP(w, r2)
	default:
		h.K7.ServeHTTP(w, r)
	}
}

func isModulePath(p string) bool {
	switch p {
	case "/dosing", "/dosing/", "/calculator", "/calculator/", "/power", "/power/", "/water", "/water/", "/system", "/system/", "/thread", "/thread/":
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
		"dosing":     {"魔點四頭滴定", "校正、容器、排程與手動滴定", "完整模擬模式；BLE 實機驗證待設備旁操作。"},
		"calculator": {"滴定計算工具", "水量、濃度與安全劑量換算", "純計算；不會啟動、設定或傳送資料到滴定機。"},
		"power":      {"電源監控", "三組排插與 18 路設備", "Home Assistant 即時狀態與 Hub 本機歷史。"},
		"water":      {"水質、水溫與換水", "樹莓派本機資料庫", "手動紀錄、完整歷史與獨立溫度來源。"},
		"system":     {"系統狀態", "連線、版本、備份與 OTA", "檢查資料來源、資料庫及更新狀態。"},
		"thread":     {"Thread / Matter", "ESP32-C6 RCP與樹莓派OTBR", "管理USB無線電、Thread邊界路由器與HA Matter連線。"},
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
	// K7 is intentionally kept on its proven standalone UI. Force the browser
	// to reload that document after a Hub OTA so stale mixed assets cannot make
	// the legacy controller appear unstyled or partially upgraded.
	w.Header().Set("Cache-Control", "no-store")
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
			"water_quality":  "available",
			"temperature":    "available",
			"dosing":         "simulation_available",
		},
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
