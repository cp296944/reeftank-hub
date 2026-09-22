// readrobust.go is a pi-bridge ADDITION — not part of the vendored upstream
// client.go, so it does not affect tools/check_k7tcp_sync.py.
//
// Upstream Client.ReadAll() sends one 0x1008 and then drain-reads for a fixed
// 5 s, so it pays the full timeout on every call and fails hard when the lamp
// answers the first read with a stale/short packet. Observed on a real K7 Pro,
// the lamp replies with a 4-byte write-ack ("AB AA A5 A1") *then* the ~222-byte
// state frame ("AB AA 10 08 <6 manual> <count=24> <24×8 slots> <auto> <name>
// … BB") as a separate read. The vendor Android app re-sends allRead until it
// sees a frame > 214 bytes starting AB AA (ConnectThread's COMMAND_RESEND).
//
// ReadAllRobust mirrors that: up to 4 attempts, each returning the moment a
// decodable frame is in hand, and it locates the frame inside the buffer so the
// leading ack / echo is skipped.
package k7tcp

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// ReadAllRobust is a retrying, frame-aware replacement for Client.ReadAll().
func ReadAllRobust(c Client) (LampState, error) {
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		st, err := readAllAttempt(c)
		if err == nil {
			return st, nil
		}
		last = err
		time.Sleep(250 * time.Millisecond)
	}
	return LampState{}, fmt.Errorf("read-all failed after 4 tries: %w", last)
}

func readAllAttempt(c Client) (LampState, error) {
	conn, err := c.connect()
	if err != nil {
		return LampState{}, err
	}
	defer conn.Close()

	if err := sendPacket(conn, cmdAllRead, nil); err != nil {
		return LampState{}, err
	}

	deadline := time.Now().Add(2500 * time.Millisecond)
	_ = conn.SetReadDeadline(deadline)
	buf := make([]byte, 0, 384)
	tmp := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, rerr := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if fr := frameStart(buf); fr != nil {
				if st, derr := decodeState(fr); derr == nil && st.Valid {
					return st, nil
				}
			}
		}
		if rerr != nil {
			var ne net.Error
			if errors.As(rerr, &ne) && ne.Timeout() {
				break
			}
			break
		}
	}
	if fr := frameStart(buf); fr != nil {
		if st, derr := decodeState(fr); derr == nil && st.Valid {
			return st, nil
		}
	}
	return LampState{}, fmt.Errorf("no valid read-all frame (%d bytes)", len(buf))
}

// frameStart returns b sliced to the first "AB AA" whose slot-count byte
// (frameStart+10) is 24 — the real state frame, past any leading write-ack
// ("AB AA A5 A1", byte+10 not 24) or model-ID echo ("AB AA 'K' '7' …").
// Returns nil when no such start is present yet; decodeState still validates the
// terminator, so an incomplete frame is treated as "keep reading".
func frameStart(b []byte) []byte {
	for i := 0; i+11 <= len(b); i++ {
		if b[i] == respMagic[0] && b[i+1] == respMagic[1] && b[i+10] == Slots {
			return b[i:]
		}
	}
	return nil
}
