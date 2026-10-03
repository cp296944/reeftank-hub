package version

import "testing"

func TestLocalHistory(t *testing.T) {
	h := LocalHistory()
	if len(h) != 49 {
		t.Fatal(len(h))
	}
	seen := map[string]bool{}
	for _, r := range h {
		tag := r["tag"].(string)
		if seen[tag] || r["notes"] == "" || r["date"] == "" || r["url"] != "" || r["embedded"] != true {
			t.Fatal(r)
		}
		seen[tag] = true
	}
	if h[0]["tag"] != "hub-v1.0.0" || h[0]["local"] != false {
		t.Fatal(h[0])
	}
	if h[1]["tag"] != "hub-v0.12.0-jebao.12-release-history" || h[1]["local"] != true {
		t.Fatal(h[1])
	}
	if !seen["hub-v0.1.0"] || !seen["hub-v0.12.0"] {
		t.Fatal("official release history is incomplete")
	}
}
