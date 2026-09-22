package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatesAndSwitch(t *testing.T) {
	var switched string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing bearer token")
		}
		switch r.URL.Path {
		case "/api/states":
			_ = json.NewEncoder(w).Encode([]State{{EntityID: "switch.test", State: "on"}})
		case "/api/services/switch/turn_off":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			switched = body["entity_id"]
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "secret")
	states, err := c.States(context.Background())
	if err != nil || states["switch.test"].State != "on" {
		t.Fatalf("states=%v err=%v", states, err)
	}
	if err := c.SetSwitch(context.Background(), "switch.test", false); err != nil || switched != "switch.test" {
		t.Fatalf("switch=%q err=%v", switched, err)
	}
}
