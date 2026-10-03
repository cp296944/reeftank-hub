package jebao

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"
)

// Feeding uses the model's FeedSwitch id=2 and preserves FeedTime. Live
// feeding behaviour is intentionally left for the owner to verify on site.
func feedingPayload(before []byte, enabled bool, seq []byte) ([]byte, error) {
	if len(before) != 302 || len(seq) != 4 {
		return nil, errors.New("未驗證的主馬狀態格式")
	}
	if enabled && before[0]&1 == 0 {
		return nil, errors.New("主馬目前關閉，請先在 App 確認")
	}
	if enabled && before[0]&8 != 0 {
		return nil, errors.New("排程啟用中，請先在 App 確認")
	}
	values := append([]byte(nil), before[:301]...)
	if enabled {
		values[0] |= 4
	} else {
		values[0] &^= 4
	}
	flags := make([]byte, 8)
	flags[7] = 1 << 2
	p := append(append([]byte(nil), seq...), 1)
	p = append(p, flags...)
	return append(p, values...), nil
}

func feedingConnection(ctx context.Context, c net.Conn, enabled bool) (map[string]any, error) {
	return writeConnection(ctx, c, func(b, seq []byte) ([]byte, error) { return feedingPayload(b, enabled, seq) }, func(before, after []byte) error {
		if (after[0]&4 != 0) != enabled {
			return errors.New("設備讀回餵食旗標與設定不符，請查看 App")
		}
		// AutoMode may change as the device enters/exits feeding. Do not write it.
		if after[0]&0x0b != before[0]&0x0b {
			return errors.New("設備開關或控制／排程旗標發生變化，請查看 App")
		}
		return nil
	})
}
func SetReturnFeeding(ctx context.Context, id Identity, enabled bool) (map[string]any, error) {
	if id.ProductKey != mdpKey || id.MAC != "e8:db:84:f4:cb:4c" {
		return nil, errors.New("此設備尚未開放餵食控制")
	}
	c, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(id.IP, "12416"))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return feedingConnection(ctx, c, enabled)
}
func (m *Monitor) registerFeeding(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/hub/jebao/return-pump/feeding", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.Enabled == nil {
			reply(w, 400, map[string]string{"error": "enabled 須為 true 或 false"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		attrs, err := m.controlReturn(ctx, func(ctx context.Context, id Identity) (map[string]any, error) {
			return m.writeFeeding(ctx, id, *in.Enabled)
		})
		if err != nil {
			reply(w, 409, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, map[string]any{"enabled": attrs["FeedSwitch"], "feed_time": attrs["FeedTime"], "verified_local": true, "cloud_verified": false, "behaviour_verified": false})
	})
}
