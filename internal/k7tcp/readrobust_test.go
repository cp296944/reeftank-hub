package k7tcp

import (
	"net"
	"strconv"
	"testing"
	"time"
)

// validFrame builds a well-formed state response as a real K7 Pro sends it:
// AB AA 10 08 | 6 manual | count=24 | 24×8 slots | auto | 11-byte name | 6 pad | BB.
func validFrame(name string) []byte {
	b := []byte{0xAB, 0xAA, 0x10, 0x08}
	b = append(b, 50, 50, 50, 50, 50, 50) // manual
	b = append(b, 24)                     // slot count
	for i := 0; i < 24; i++ {
		b = append(b, byte(i), 0, 0, 0, 0, 0, 0, 0)
	}
	b = append(b, 1) // auto
	for len(name) < 11 {
		name += "0"
	}
	b = append(b, []byte(name[:11])...)
	b = append(b, 0, 0, 0, 0, 0, 0) // trailing pad
	b = append(b, pktEnd)
	return b
}

// serveOnce writes each of `writes` (with a small gap) on the first connection.
func serveOnce(t *testing.T, writes ...[]byte) Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _ = c.Read(make([]byte, 64)) // consume the allRead request
		for _, w := range writes {
			_, _ = c.Write(w)
			time.Sleep(20 * time.Millisecond)
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	return New(host, p, 2*time.Second)
}

func TestReadAllRobust_CleanFrame(t *testing.T) {
	c := serveOnce(t, validFrame("K7_Pro42110"))
	st, err := ReadAllRobust(c)
	if err != nil {
		t.Fatalf("ReadAllRobust: %v", err)
	}
	if st.Name != "K7_Pro42110" || !st.Valid || st.Manual != [Channels]int{50, 50, 50, 50, 50, 50} {
		t.Errorf("decoded name=%q manual=%v valid=%v", st.Name, st.Manual, st.Valid)
	}
}

// The real lamp prepends a 4-byte write-ack; frameStart must skip it.
func TestReadAllRobust_AckThenFrame(t *testing.T) {
	ack := []byte{0xAB, 0xAA, 0xA5, 0xA1}
	c := serveOnce(t, ack, validFrame("K7_Pro42113"))
	st, err := ReadAllRobust(c)
	if err != nil {
		t.Fatalf("ReadAllRobust: %v", err)
	}
	if st.Name != "K7_Pro42113" || st.Manual != [Channels]int{50, 50, 50, 50, 50, 50} {
		t.Errorf("misaligned decode: name=%q manual=%v", st.Name, st.Manual)
	}
}

// ack + frame coalesced into one TCP segment.
func TestReadAllRobust_AckAndFrameCoalesced(t *testing.T) {
	ack := []byte{0xAB, 0xAA, 0xA5, 0xA1}
	c := serveOnce(t, append(ack, validFrame("K7_Pro42113")...))
	st, err := ReadAllRobust(c)
	if err != nil {
		t.Fatalf("ReadAllRobust: %v", err)
	}
	if st.Name != "K7_Pro42113" {
		t.Errorf("name=%q, want K7_Pro42113", st.Name)
	}
}

func TestReadAllRobust_JunkThenFrame(t *testing.T) {
	echo := []byte{0xAB, 0xAA, 'K', '7', 'P', 'r', 'o', pktEnd}
	short := make([]byte, 40)
	c := serveOnce(t, echo, short, validFrame("K7_Pro99999"))
	st, err := ReadAllRobust(c)
	if err != nil {
		t.Fatalf("ReadAllRobust should skip junk and find the frame: %v", err)
	}
	if st.Name != "K7_Pro99999" {
		t.Errorf("name=%q, want K7_Pro99999", st.Name)
	}
}

func TestReadAllRobust_RetriesThenFails(t *testing.T) {
	start := time.Now()
	c := New("127.0.0.1", 1, 300*time.Millisecond)
	if _, err := ReadAllRobust(c); err == nil {
		t.Fatal("expected an error when nothing is listening")
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Errorf("took %v, retry loop not bounded", d)
	}
}
