package jebao

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const mdpKey = "02039876751049deb404d1d89221ec4b"

// SetReturnSpeed changes only the tested MDP Motor_Speed attribute. It never
// changes power, mode, feeding, timer or schedule slots, and never retries writes.
func SetReturnSpeed(ctx context.Context, id Identity, speed int) (map[string]any, error) {
	if id.ProductKey != mdpKey || id.MAC != "e8:db:84:f4:cb:4c" {
		return nil, errors.New("此設備尚未驗證速度控制")
	}
	if speed < 1 || speed > 100 {
		return nil, errors.New("速度須為 1–100%")
	}
	c, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(id.IP, "12416"))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return setSpeedConnection(ctx, c, speed)
}

func speedPayload(before []byte, speed int, seq []byte) ([]byte, error) {
	if len(before) != 302 || len(seq) != 4 || speed < 1 || speed > 100 {
		return nil, errors.New("未驗證的主馬狀態格式或速度")
	}
	if before[0]&1 == 0 {
		return nil, errors.New("主馬目前關閉，請先在 App 確認")
	}
	if before[0]&12 != 0 {
		return nil, errors.New("餵食或排程啟用中，請先在 App 確認")
	}
	flags := make([]byte, 8)
	flags[7] = 1 << 5
	values := append([]byte(nil), before[:301]...)
	values[1] = byte(speed)
	p := append(append([]byte(nil), seq...), 1)
	p = append(p, flags...)
	return append(p, values...), nil
}

func setSpeedConnection(ctx context.Context, c net.Conn, speed int) (map[string]any, error) {
	return writeConnection(ctx, c, func(b, seq []byte) ([]byte, error) { return speedPayload(b, speed, seq) }, func(before, after []byte) error {
		if after[1] != byte(speed) {
			return errors.New("設備讀回速度與設定不符，請查看 App")
		}
		if after[0] != before[0] {
			return errors.New("設備模式旗標發生變化，請查看 App")
		}
		return nil
	})
}
func writeConnection(ctx context.Context, c net.Conn, payload func([]byte, []byte) ([]byte, error), verify func([]byte, []byte) error) (map[string]any, error) {
	return writeModelConnection(ctx, c, mdpKey, 302, payload, verify)
}
func writeModelConnection(ctx context.Context, c net.Conn, key string, statusSize int, payload func([]byte, []byte) ([]byte, error), verify func([]byte, []byte) error) (map[string]any, error) {
	deadline := time.Now().Add(12 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	done := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer done()
	r := bufio.NewReader(c)
	exchange := func(cmd, want uint16, p []byte) ([]byte, error) {
		if _, err := io.Copy(c, bytes.NewReader(packet(cmd, p))); err != nil {
			return nil, err
		}
		for i := 0; i < 24; i++ {
			got, b, err := readFrame(r)
			if err != nil {
				return nil, err
			}
			if got == want {
				return b, nil
			}
		}
		return nil, errors.New("設備未回覆預期命令")
	}
	pass, err := exchange(6, 7, nil)
	if err != nil {
		return nil, err
	}
	if len(pass) < 2 || int(binary.BigEndian.Uint16(pass))+2 != len(pass) || len(pass) > 66 || len(pass) == 2 {
		return nil, errors.New("invalid passcode")
	}
	login, err := exchange(8, 9, pass)
	if err != nil {
		return nil, err
	}
	if len(login) == 0 || login[0] != 0 {
		return nil, errors.New("LAN login rejected")
	}
	read := func() ([]byte, error) {
		b, e := exchange(0x90, 0x91, []byte{2})
		if e != nil {
			return nil, e
		}
		if len(b) != statusSize+1 || (b[0] != 3 && b[0] != 4) {
			return nil, errors.New("未驗證的主馬狀態格式")
		}
		return b[1:], nil
	}
	before, err := read()
	if err != nil {
		return nil, err
	}
	seq := make([]byte, 4)
	if _, err = rand.Read(seq); err != nil {
		return nil, err
	}
	p, err := payload(before, seq)
	if err != nil {
		return nil, err
	}
	ack, err := exchange(0x93, 0x94, p)
	if err != nil {
		return nil, fmt.Errorf("寫入結果未確認；請重新讀取或查看 App，未自動重送：%w", err)
	}
	if len(ack) < 4 || !bytes.Equal(ack[:4], seq) {
		return nil, errors.New("寫入回覆不符；請查看 App，未自動重送")
	}
	after, err := read()
	if err != nil {
		return nil, fmt.Errorf("設備已回覆寫入，但讀回失敗：%w", err)
	}
	if err := verify(before, after); err != nil {
		return nil, err
	}
	m, _ := loadModel(key)
	return m.decode(after)
}

func (m *Monitor) setSpeed(ctx context.Context, speed int) (map[string]any, error) {
	return m.controlReturn(ctx, func(ctx context.Context, id Identity) (map[string]any, error) { return m.writeSpeed(ctx, id, speed) })
}
func (m *Monitor) controlReturn(ctx context.Context, write func(context.Context, Identity) (map[string]any, error)) (map[string]any, error) {
	return m.controlDevice(ctx, 0, func(key string) bool { return key == mdpKey }, write)
}
func (m *Monitor) controlDevice(ctx context.Context, index int, acceptKey func(string) bool, write func(context.Context, Identity) (map[string]any, error)) (map[string]any, error) {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return nil, errors.New("正在讀取設備，請稍後再試")
	}
	d := m.devices[index]
	host := m.settings.Hosts[d.ID]
	if host == "" {
		host = d.Identity.IP
	}
	if host == "" {
		m.mu.Unlock()
		return nil, errors.New("尚未取得設備 IP")
	}
	m.busy = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.busy = false; m.mu.Unlock() }()
	ids, err := m.discover(ctx, host)
	if err != nil {
		return nil, err
	}
	var id Identity
	for _, v := range ids {
		if v.MAC == d.MAC && acceptKey(v.ProductKey) {
			id = v
			break
		}
	}
	if id.IP == "" {
		return nil, errors.New("設備 IP 未回應預期 MAC 與產品識別碼")
	}
	attrs, err := write(ctx, id)
	if err != nil {
		m.failed(index, err.Error())
		return nil, err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	v := &m.devices[index]
	v.Identity = id
	v.Attributes = attrs
	v.Connected = true
	v.Error = ""
	v.LastSuccess = &now
	v.LastAttempt = &now
	m.mu.Unlock()
	return attrs, nil
}
