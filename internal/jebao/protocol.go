// Package jebao implements read-only Gizwits LAN monitoring. No pump write
// commands, schedule changes or cloud credentials are supported.
package jebao

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const maxFrame = 65536

func packet(cmd uint16, payload []byte) []byte {
	body := append([]byte{0, byte(cmd >> 8), byte(cmd)}, payload...)
	p := []byte{0, 0, 0, 3}
	for n := len(body); ; n >>= 7 {
		v := byte(n & 127)
		if n > 127 {
			v |= 128
		}
		p = append(p, v)
		if n <= 127 {
			break
		}
	}
	return append(p, body...)
}

func readFrame(r *bufio.Reader) (uint16, []byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	if !bytes.Equal(header, []byte{0, 0, 0, 3}) {
		return 0, nil, errors.New("invalid LAN frame header")
	}
	n := 0
	for i := 0; i < 3; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		n |= int(b&127) << (7 * i)
		if n > maxFrame {
			return 0, nil, errors.New("LAN frame too large")
		}
		if b&128 == 0 {
			if n < 3 {
				return 0, nil, errors.New("LAN frame too short")
			}
			body := make([]byte, n)
			if _, err := io.ReadFull(r, body); err != nil {
				return 0, nil, err
			}
			return binary.BigEndian.Uint16(body[1:3]), body[3:], nil
		}
	}
	return 0, nil, errors.New("invalid LAN variable length")
}

type Identity struct {
	IP         string `json:"ip"`
	MAC        string `json:"mac"`
	DID        string `json:"did"`
	Firmware   string `json:"firmware"`
	ProductKey string `json:"product_key"`
}

func parseIdentity(data []byte, ip string) (Identity, error) {
	cmd, payload, err := readFrame(bufio.NewReader(bytes.NewReader(data)))
	if err != nil || cmd != 4 {
		return Identity{}, errors.New("invalid discovery response")
	}
	fields := make([][]byte, 4)
	for i := range fields {
		if len(payload) < 2 {
			return Identity{}, errors.New("truncated discovery field")
		}
		n := int(binary.BigEndian.Uint16(payload))
		payload = payload[2:]
		if n > len(payload) {
			return Identity{}, errors.New("truncated discovery data")
		}
		fields[i], payload = payload[:n], payload[n:]
	}
	if len(fields[1]) != 6 || len(fields[0]) == 0 {
		return Identity{}, errors.New("missing device identity")
	}
	pk := strings.ToLower(string(fields[3]))
	if len(pk) != 32 {
		return Identity{}, errors.New("unsupported product key format")
	}
	if _, err := hex.DecodeString(pk); err != nil {
		return Identity{}, errors.New("invalid product key")
	}
	return Identity{IP: ip, MAC: net.HardwareAddr(fields[1]).String(), DID: string(fields[0]), Firmware: string(fields[2]), ProductKey: pk}, nil
}

// Discover sends only a discovery request on each local IPv4 subnet. A directed
// IP may be used for VLANs where broadcast is unavailable.
func Discover(ctx context.Context, target string) ([]Identity, error) {
	targets := map[string]bool{}
	if target != "" {
		ip := net.ParseIP(target)
		if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
			return nil, errors.New("a private IPv4 address is required")
		}
		targets[target] = true
	} else {
		interfaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagBroadcast == 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				ip, network, err := net.ParseCIDR(addr.String())
				if err != nil || ip.To4() == nil || !ip.IsPrivate() {
					continue
				}
				v := ip.To4()
				broadcast := make(net.IP, 4)
				for i := range broadcast {
					broadcast[i] = v[i] | ^network.Mask[i]
				}
				targets[broadcast.String()] = true
			}
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("no active private IPv4 LAN interface")
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if err := enableBroadcast(c); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	done := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer done()
	sent := 0
	var sendErr error
	for ip := range targets {
		if _, err := c.WriteToUDP(packet(3, nil), &net.UDPAddr{IP: net.ParseIP(ip), Port: 12414}); err != nil {
			sendErr = err
		} else {
			sent++
		}
	}
	if sent == 0 {
		return nil, fmt.Errorf("discovery send failed: %w", sendErr)
	}
	results := map[string]Identity{}
	buffer := make([]byte, maxFrame)
	for {
		n, addr, err := c.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if e, ok := err.(net.Error); ok && e.Timeout() {
				break
			}
			return nil, err
		}
		if target != "" && addr.IP.String() != target {
			continue
		}
		if id, err := parseIdentity(buffer[:n], addr.IP.String()); err == nil {
			results[id.MAC] = id
		}
	}
	out := make([]Identity, 0, len(results))
	for _, id := range results {
		out = append(out, id)
	}
	return out, nil
}

func ReadStatus(ctx context.Context, id Identity) (map[string]any, error) {
	m, err := loadModel(id.ProductKey)
	if err != nil {
		return nil, err
	}
	c, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(id.IP, "12416"))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return readStatusConnection(ctx, c, m)
}

func readStatusConnection(ctx context.Context, c net.Conn, m model) (map[string]any, error) {
	deadline := time.Now().Add(7 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	done := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer done()
	r := bufio.NewReader(c)
	exchange := func(cmd, want uint16, payload []byte) ([]byte, error) {
		if _, err := io.Copy(c, bytes.NewReader(packet(cmd, payload))); err != nil {
			return nil, err
		}
		for i := 0; i < 24; i++ {
			got, p, err := readFrame(r)
			if err != nil {
				return nil, err
			}
			if got == want {
				return p, nil
			}
		}
		return nil, errors.New("expected LAN response missing")
	}
	pass, err := exchange(6, 7, nil)
	if err != nil {
		return nil, fmt.Errorf("LAN authentication: %w", err)
	}
	if len(pass) < 2 {
		return nil, errors.New("invalid passcode response")
	}
	n := int(binary.BigEndian.Uint16(pass))
	if n == 0 || n > 64 || len(pass) != n+2 {
		return nil, errors.New("invalid passcode length")
	}
	login, err := exchange(8, 9, pass)
	if err != nil {
		return nil, err
	}
	if len(login) < 1 || login[0] != 0 {
		return nil, errors.New("LAN login rejected")
	}
	status, err := exchange(0x90, 0x91, []byte{2})
	if err != nil {
		return nil, err
	}
	if len(status) == 0 || (status[0] != 3 && status[0] != 4) {
		return nil, errors.New("unexpected status action")
	}
	return m.decode(status[1:])
}
