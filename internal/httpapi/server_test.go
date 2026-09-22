package httpapi

import "testing"

func TestDeviceFromLampName(t *testing.T) {
	cases := map[string]string{
		"K7_Pro42113": "k7pro",
		"k7pro-abc":   "k7pro",
		"K7Pro":       "k7pro",
		"k7m1234":     "k7mini",
		"K7M_xyz":     "k7mini",
		"x4-9999":     "",
		"":            "",
	}
	for name, want := range cases {
		if got := deviceFromLampName(name); got != want {
			t.Errorf("deviceFromLampName(%q) = %q, want %q", name, got, want)
		}
	}
}
