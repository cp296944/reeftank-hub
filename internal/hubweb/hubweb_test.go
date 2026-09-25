package hubweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHubRoutesAndK7Compatibility(t *testing.T) {
	k7 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-K7-Path", r.URL.Path)
		_, _ = w.Write([]byte("k7"))
	})
	h := New(Options{K7: k7, Version: "hub-vtest", InstallRoot: t.TempDir()})

	tests := []struct {
		path       string
		wantStatus int
		contains   string
		k7Path     string
	}{
		{"/", 200, "ReefTank Hub", ""},
		{"/hub-assets/console.css", 200, "system-monitor", ""},
		{"/api/hub/status", 200, `"version":"hub-vtest"`, ""},
		{"/api/bootstrap/status", 200, `"required":true`, ""},
		{"/K7/", 200, "k7", "/static/"},
		{"/K7/mobile.html", 200, "k7", "/static/mobile.html"},
		{"/dosing/", 200, "魔點四頭滴定", ""},
		{"/calculator/", 200, "滴定計算工具", ""},
		{"/power/", 200, "18 路設備", ""},
		{"/water/", 200, "樹莓派本機資料庫", ""},
		{"/system/", 200, "系統狀態", ""},
		{"/thread/", 200, "ESP32-C6 RCP", ""},
		{"/api/version", 200, "k7", "/api/version"},
		{"/static/index.html", 200, "k7", "/static/index.html"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rr.Code != tc.wantStatus || !strings.Contains(rr.Body.String(), tc.contains) {
				t.Fatalf("%s = %d %q", tc.path, rr.Code, rr.Body.String())
			}
			if tc.k7Path != "" && rr.Header().Get("X-K7-Path") != tc.k7Path {
				t.Fatalf("%s forwarded as %q, want %q", tc.path, rr.Header().Get("X-K7-Path"), tc.k7Path)
			}
		})
	}
}

func TestK7Redirect(t *testing.T) {
	h := New(Options{K7: http.NotFoundHandler()})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/K7", nil))
	if rr.Code != http.StatusPermanentRedirect || rr.Header().Get("Location") != "/K7/" {
		t.Fatalf("redirect = %d %q", rr.Code, rr.Header().Get("Location"))
	}
}

func TestK7PageDisablesCache(t *testing.T) {
	h := New(Options{K7: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("k7"))
	})})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/K7/", nil))
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
