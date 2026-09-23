package highfreq

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

type outletReading struct {
	Voltage float64
	Current float64
	Power   float64
}

type hs300Client struct{ timeout time.Duration }

func (c hs300Client) childIDs(ctx context.Context, host string) ([]string, error) {
	var out struct {
		System struct {
			GetSysinfo struct {
				ErrCode  int `json:"err_code"`
				Children []struct {
					ID string `json:"id"`
				} `json:"children"`
			} `json:"get_sysinfo"`
		} `json:"system"`
	}
	if err := c.query(ctx, host, map[string]any{"system": map[string]any{"get_sysinfo": map[string]any{}}}, &out); err != nil {
		return nil, err
	}
	if out.System.GetSysinfo.ErrCode != 0 || len(out.System.GetSysinfo.Children) != 6 {
		return nil, errors.New("HS300 returned invalid child list")
	}
	ids := make([]string, 6)
	for i, v := range out.System.GetSysinfo.Children {
		ids[i] = v.ID
	}
	return ids, nil
}

func (c hs300Client) energy(ctx context.Context, host, childID string) (outletReading, error) {
	var out struct {
		Emeter struct {
			GetRealtime struct {
				ErrCode   int     `json:"err_code"`
				VoltageMV float64 `json:"voltage_mv"`
				CurrentMA float64 `json:"current_ma"`
				PowerMW   float64 `json:"power_mw"`
			} `json:"get_realtime"`
		} `json:"emeter"`
	}
	req := map[string]any{"context": map[string]any{"child_ids": []string{childID}}, "emeter": map[string]any{"get_realtime": map[string]any{}}}
	if err := c.query(ctx, host, req, &out); err != nil {
		return outletReading{}, err
	}
	v := out.Emeter.GetRealtime
	if v.ErrCode != 0 {
		return outletReading{}, fmt.Errorf("HS300 emeter error %d", v.ErrCode)
	}
	return outletReading{Voltage: v.VoltageMV / 1000, Current: v.CurrentMA / 1000, Power: v.PowerMW / 1000}, nil
}

func (c hs300Client) query(ctx context.Context, host string, request, response any) error {
	b, err := json.Marshal(request)
	if err != nil {
		return err
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "9999"))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	packet := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(packet, uint32(len(b)))
	key := byte(171)
	for i, v := range b {
		key ^= v
		packet[4+i] = key
	}
	if _, err = conn.Write(packet); err != nil {
		return err
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(conn, header); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(header)
	if n == 0 || n > 1<<20 {
		return fmt.Errorf("invalid HS300 response length %d", n)
	}
	cipher := make([]byte, n)
	if _, err = io.ReadFull(conn, cipher); err != nil {
		return err
	}
	plain := make([]byte, n)
	key = 171
	for i, v := range cipher {
		plain[i] = key ^ v
		key = v
	}
	if err = json.Unmarshal(plain, response); err != nil {
		return fmt.Errorf("decode HS300 response: %w", err)
	}
	return nil
}
