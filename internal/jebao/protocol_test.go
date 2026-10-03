package jebao

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

type oneByteReader struct{ r io.Reader }

func (r oneByteReader) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return r.r.Read(b)
}
func TestFrameFragmentationAndCoalescing(t *testing.T) {
	p := append(packet(0x91, bytes.Repeat([]byte{42}, 400)), packet(9, []byte{0})...)
	r := bufio.NewReader(oneByteReader{bytes.NewReader(p)})
	cmd, payload, err := readFrame(r)
	if err != nil || cmd != 0x91 || len(payload) != 400 {
		t.Fatalf("first frame %x %d %v", cmd, len(payload), err)
	}
	cmd, payload, err = readFrame(r)
	if err != nil || cmd != 9 || len(payload) != 1 {
		t.Fatalf("second frame %x %v", cmd, err)
	}
	for _, p := range [][]byte{{0, 0, 0, 3, 0}, {0, 0, 0, 3, 255, 255, 255}, {0, 0, 0, 4, 3, 0, 0, 9}} {
		if _, _, err = readFrame(bufio.NewReader(bytes.NewReader(p))); err == nil {
			t.Fatal("accepted malformed frame")
		}
	}
}
func TestDiscoveryFields(t *testing.T) {
	var p []byte
	for _, f := range [][]byte{[]byte("device-123"), {0xe8, 0xdb, 0x84, 0xf4, 0xcb, 0x4c}, []byte("04020B28"), []byte("02039876751049deb404d1d89221ec4b")} {
		p = binary.BigEndian.AppendUint16(p, uint16(len(f)))
		p = append(p, f...)
	}
	id, err := parseIdentity(packet(4, p), "192.168.0.2")
	if err != nil || id.MAC != "e8:db:84:f4:cb:4c" || id.DID != "device-123" {
		t.Fatalf("identity %+v %v", id, err)
	}
	if _, err := parseIdentity(packet(4, p[:len(p)-1]), "192.168.0.2"); err == nil {
		t.Fatal("accepted truncated identity")
	}
}
func TestModelCrossByteAndMissingFaults(t *testing.T) {
	m, err := loadModel("f0d844ab0d4947ac9527a286160bc705")
	if err != nil {
		t.Fatal(err)
	}
	// The first two bytes are one big-endian bit group. Linkage=2 crosses bit 7.
	p := make([]byte, 393)
	p[0] = 1
	p[1] = 1
	p[2] = 75
	p[3] = 50
	p[4] = 10
	values, err := m.decode(p)
	if err != nil {
		t.Fatal(err)
	}
	if values["Linkage"] != uint64(2) || values["SwitchON"] != true || values["Flow"] != uint64(75) {
		t.Fatalf("decoded %v", values)
	}
	if _, err := m.decode(p[:5]); err == nil {
		t.Fatal("short status must not imply clear fault flags")
	}
	if _, err := loadModel("unknown"); err == nil {
		t.Fatal("accepted unknown model")
	}
}
func TestHandshakeNeverWritesPumpAttributes(t *testing.T) {
	m, err := loadModel("02039876751049deb404d1d89221ec4b")
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		r := bufio.NewReader(server)
		steps := []struct {
			cmd      uint16
			payload  []byte
			reply    uint16
			response []byte
		}{{6, nil, 7, append([]byte{0, 10}, []byte("abcdefghij")...)}, {8, append([]byte{0, 10}, []byte("abcdefghij")...), 9, []byte{0}}, {0x90, []byte{2}, 0x91, append([]byte{3}, make([]byte, 302)...)}}
		steps[2].response[1] = 0x11
		steps[2].response[2] = 75
		for _, s := range steps {
			cmd, p, err := readFrame(r)
			if err != nil {
				errs <- err
				return
			}
			if cmd != s.cmd || !bytes.Equal(p, s.payload) {
				errs <- io.ErrUnexpectedEOF
				return
			}
			for _, b := range packet(s.reply, s.response) {
				if _, err := server.Write([]byte{b}); err != nil {
					errs <- err
					return
				}
			}
		}
		errs <- nil
	}()
	values, err := readStatusConnection(ctx, client, m)
	if err != nil {
		t.Fatal(err)
	}
	if values["Motor_Speed"] != uint64(75) {
		t.Fatal(values)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}
