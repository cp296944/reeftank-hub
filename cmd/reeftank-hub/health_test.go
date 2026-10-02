package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalHealthChecksCoreEndpoints(t *testing.T) {
	for _, bad := range []string{"", "/healthz", "/api/hub/storage/status", "/hub-assets/app.js"} {
		t.Run(bad, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if bad != "" && r.URL.Path == bad {
					http.Error(w, "unavailable", 503)
					return
				}
				w.Write([]byte("ok"))
			}))
			defer srv.Close()
			err := checkLocalHTTP(context.Background(), strings.TrimPrefix(srv.URL, "http://"))
			if (err != nil) != (bad != "") {
				t.Fatalf("health error=%v bad=%q", err, bad)
			}
		})
	}
}
