package jebao

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func waveStatus(key string) []byte {
	spec, _ := flowModel(key)
	b := make([]byte, spec.size)
	if key == "50dbc92221fd4d33ae69a1fedd43b555" {
		b[0] = 1
		b[1] = 6
	} else {
		b[1] = 1
	}
	b[2] = 35
	b[10] = 42
	return b
}
func TestFlowFlagsAndGuards(t *testing.T) {
	for _, key := range []string{"50dbc92221fd4d33ae69a1fedd43b555", "54114ccdac1e41c0bb17e222887c07ba"} {
		b := waveStatus(key)
		spec, _ := flowModel(key)
		p, e := flowPayload(key, b, 40, make([]byte, 4))
		if e != nil {
			t.Fatal(e)
		}
		expectedFlags := make([]byte, 8)
		expectedFlags[7-spec.flag/8] = 1 << uint(spec.flag%8)
		if !bytes.Equal(p[5:13], expectedFlags) {
			t.Fatal("wrong model flags")
		}
		want := append([]byte(nil), b[:spec.end]...)
		want[2] = 40
		if !bytes.Equal(p[13:], want) || b[2] != 35 {
			t.Fatal("unrelated state changed")
		}
		off := 0
		timer := byte(2)
		if spec.flag == 8 {
			off = 1
			timer = 8
		}
		b[off] |= timer
		if _, e := flowPayload(key, b, 40, make([]byte, 4)); e == nil {
			t.Fatal("accepted schedule")
		}
		b = waveStatus(key)
		if spec.flag == 4 {
			b[0] |= 4
		} else {
			b[1] |= 128
		}
		if _, e := flowPayload(key, b, 40, make([]byte, 4)); e == nil {
			t.Fatal("accepted linkage")
		}
		b = waveStatus(key)
		b[off] &^= 1
		if _, e := flowPayload(key, b, 40, make([]byte, 4)); e == nil {
			t.Fatal("accepted off")
		}
	}
}
func TestFlowHandshakeBothModels(t *testing.T) {
	for _, key := range []string{"50dbc92221fd4d33ae69a1fedd43b555", "54114ccdac1e41c0bb17e222887c07ba"} {
		t.Run(key, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			before := waveStatus(key)
			go func() {
				r := bufio.NewReader(b)
				for i := 0; i < 5; i++ {
					cmd, p, e := readFrame(r)
					if e != nil {
						return
					}
					var reply uint16
					var data []byte
					switch i {
					case 0:
						reply = 7
						data = []byte{0, 1, 'x'}
					case 1:
						reply = 9
						data = []byte{0}
					case 2:
						reply = 0x91
						data = append([]byte{3}, before...)
					case 3:
						want, e := flowPayload(key, before, 40, p[:4])
						if e != nil || cmd != 0x93 || !bytes.Equal(want, p) {
							t.Error("write mismatch")
						}
						reply = 0x94
						data = p[:4]
					case 4:
						reply = 0x91
						after := append([]byte(nil), before...)
						after[2] = 40
						data = append([]byte{3}, after...)
					}
					_, _ = b.Write(packet(reply, data))
				}
			}()
			attrs, e := flowConnection(ctx, a, key, 40)
			if e != nil || attrs["Flow"] != uint64(40) {
				t.Fatal(attrs, e)
			}
		})
	}
}
func TestFlowAPIOnlyRegisteredWaveIdentity(t *testing.T) {
	m, _ := Open("")
	m.settings.Hosts["wavemaker-2"] = "192.168.0.65"
	m.discover = func(context.Context, string) ([]Identity, error) {
		return []Identity{{IP: "192.168.0.65", MAC: m.devices[2].MAC, ProductKey: mdpKey}}, nil
	}
	m.writeFlow = func(context.Context, Identity, int) (map[string]any, error) {
		t.Fatal("write wrong product")
		return nil, nil
	}
	mux := http.NewServeMux()
	m.Register(mux)
	for _, body := range []string{`{}`, `{"flow":0}`, `{"flow":101}`, `{"flow":40.5}`, `{"flow":40,"mode":1}`} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/wavemakers/wavemaker-2/flow", strings.NewReader(body)))
		if rr.Code != 400 {
			t.Fatal(rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/wavemakers/wavemaker-2/flow", strings.NewReader(`{"flow":40}`)))
	if rr.Code != 409 {
		t.Fatal(rr.Code)
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/wavemakers/return-pump-1/flow", strings.NewReader(`{"flow":40}`)))
	if rr.Code != 404 {
		t.Fatal(rr.Code)
	}
}
