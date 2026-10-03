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

func TestSpeedPayloadOnlySelectsMotor(t *testing.T) {
	b := make([]byte, 302)
	b[0] = 0x11
	b[1] = 83
	b[20] = 42
	p, e := speedPayload(b, 80, []byte{1, 2, 3, 4})
	if e != nil {
		t.Fatal(e)
	}
	if len(p) != 314 || !bytes.Equal(p[5:13], []byte{0, 0, 0, 0, 0, 0, 0, 32}) {
		t.Fatal("wrong attribute flags", p[:13])
	}
	want := append([]byte(nil), b[:301]...)
	want[1] = 80
	if !bytes.Equal(p[13:], want) || b[1] != 83 {
		t.Fatal("modified unrelated attributes or input")
	}
	for _, v := range []byte{0, 5, 9} {
		b[0] = v
		if _, e := speedPayload(b, 80, []byte{1, 2, 3, 4}); e == nil {
			t.Fatal("accepted off/feed/timer")
		}
	}
}
func TestSpeedHandshakeAndReadback(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	before := make([]byte, 302)
	before[0] = 0x11
	before[1] = 83
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
				if cmd != 6 {
					t.Error("passcode")
				}
				reply = 7
				data = []byte{0, 1, 'x'}
			case 1:
				if cmd != 8 {
					t.Error("login")
				}
				reply = 9
				data = []byte{0}
			case 2:
				if cmd != 0x90 {
					t.Error("read")
				}
				reply = 0x91
				data = append([]byte{3}, before...)
			case 3:
				want, e := speedPayload(before, 80, p[:4])
				if e != nil || cmd != 0x93 || !bytes.Equal(want, p) {
					t.Error("write mismatch")
				}
				reply = 0x94
				data = p[:4]
			case 4:
				if cmd != 0x90 {
					t.Error("readback")
				}
				reply = 0x91
				after := append([]byte(nil), before...)
				after[1] = 80
				data = append([]byte{3}, after...)
			}
			_, _ = b.Write(packet(reply, data))
		}
	}()
	attrs, e := setSpeedConnection(ctx, a, 80)
	if e != nil || attrs["Motor_Speed"] != uint64(80) {
		t.Fatal(attrs, e)
	}
}
func TestControlRejectsInvalidInputAndWrongIdentity(t *testing.T) {
	m, _ := Open("")
	m.settings.Hosts["return-pump-1"] = "192.168.0.55"
	m.discover = func(context.Context, string) ([]Identity, error) {
		return []Identity{{IP: "192.168.0.55", MAC: "wrong", ProductKey: mdpKey}}, nil
	}
	m.writeSpeed = func(context.Context, Identity, int) (map[string]any, error) {
		t.Fatal("write to wrong identity")
		return nil, nil
	}
	mux := http.NewServeMux()
	m.Register(mux)
	for _, body := range []string{`{}`, `{"speed":0}`, `{"speed":101}`, `{"speed":80.5}`, `{"speed":80,"mode":1}`} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/return-pump/speed", strings.NewReader(body)))
		if rr.Code != 400 {
			t.Fatal(rr.Code, body)
		}
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/return-pump/speed", strings.NewReader(`{"speed":80}`)))
	if rr.Code != 409 {
		t.Fatal(rr.Code)
	}
}
