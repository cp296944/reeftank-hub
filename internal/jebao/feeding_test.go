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

func TestFeedingPayloadPreservesOtherSettings(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		b := make([]byte, 302)
		b[0] = 0x11
		b[1] = 80
		b[2] = 10
		b[20] = 42
		if !enabled {
			b[0] |= 4
		}
		p, e := feedingPayload(b, enabled, []byte{1, 2, 3, 4})
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(p[5:13], []byte{0, 0, 0, 0, 0, 0, 0, 4}) {
			t.Fatal("wrong flags")
		}
		expected := append([]byte(nil), b[:301]...)
		if enabled {
			expected[0] |= 4
		} else {
			expected[0] &^= 4
		}
		if !bytes.Equal(p[13:], expected) || p[15] != 10 {
			t.Fatal("changed time or other settings")
		}
	}
	b := make([]byte, 302)
	if _, e := feedingPayload(b, true, make([]byte, 4)); e == nil {
		t.Fatal("started off pump")
	}
	b[0] = 9
	if _, e := feedingPayload(b, true, make([]byte, 4)); e == nil {
		t.Fatal("started scheduled pump")
	}
	if _, e := feedingPayload(b, false, make([]byte, 4)); e != nil {
		t.Fatal("must allow ending feeding", e)
	}
}
func TestFeedingHandshakeReadback(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "start", false: "stop"}[enabled], func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			before := make([]byte, 302)
			before[0] = 0x11
			before[1] = 80
			before[2] = 10
			if !enabled {
				before[0] |= 4
			}
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
						want, e := feedingPayload(before, enabled, p[:4])
						if e != nil || cmd != 0x93 || !bytes.Equal(want, p) {
							t.Error("write mismatch")
						}
						reply = 0x94
						data = p[:4]
					case 4:
						reply = 0x91
						after := append([]byte(nil), before...)
						if enabled {
							after[0] |= 4
						} else {
							after[0] &^= 4
						}
						data = append([]byte{3}, after...)
					}
					_, _ = b.Write(packet(reply, data))
				}
			}()
			attrs, e := feedingConnection(ctx, a, enabled)
			if e != nil || attrs["FeedSwitch"] != enabled || attrs["Motor_Speed"] != uint64(80) || attrs["FeedTime"] != uint64(10) {
				t.Fatal(attrs, e)
			}
		})
	}
}
func TestFeedingAPIValidationAndBusy(t *testing.T) {
	m, _ := Open("")
	mux := http.NewServeMux()
	m.Register(mux)
	for _, body := range []string{`{}`, `{"enabled":1}`, `{"enabled":true,"time":10}`} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/return-pump/feeding", strings.NewReader(body)))
		if rr.Code != 400 {
			t.Fatal(rr.Code)
		}
	}
	m.busy = true
	m.writeFeeding = func(context.Context, Identity, bool) (map[string]any, error) {
		t.Fatal("write while busy")
		return nil, nil
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/return-pump/feeding", strings.NewReader(`{"enabled":true}`)))
	if rr.Code != 409 {
		t.Fatal(rr.Code)
	}
}
