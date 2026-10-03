package jebao

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"
)

type flowSpec struct{ size, end, flag int }

func flowModel(key string) (flowSpec, bool) {
	switch key {
	case "50dbc92221fd4d33ae69a1fedd43b555":
		return flowSpec{452, 451, 4}, true
	case "54114ccdac1e41c0bb17e222887c07ba":
		return flowSpec{401, 400, 8}, true
	}
	return flowSpec{}, false
}
func flowPayload(key string, b []byte, flow int, seq []byte) ([]byte, error) {
	spec, ok := flowModel(key)
	if !ok || len(b) != spec.size || len(seq) != 4 || flow < 1 || flow > 100 {
		return nil, errors.New("不支援的造浪格式或強度；須為 1–100%")
	}
	model, err := loadModel(key)
	if err != nil {
		return nil, err
	}
	attrs, err := model.decode(b)
	if err != nil {
		return nil, err
	}
	if attrs["SwitchON"] != true {
		return nil, errors.New("造浪目前關閉，請先在 App 確認")
	}
	if attrs["TimerON"] == true {
		return nil, errors.New("排程啟用中，請先在 App 關閉排程後調整強度")
	}
	if attrs["FeedSwitch"] == true || (key == "50dbc92221fd4d33ae69a1fedd43b555" && attrs["Mode"] == uint64(7)) {
		return nil, errors.New("餵食模式啟用中，請先結束餵食")
	}
	if attrs["Linkage"] != uint64(0) {
		return nil, errors.New("聯動模式啟用中，請先在 App 設為獨立")
	}
	values := append([]byte(nil), b[:spec.end]...)
	values[2] = byte(flow)
	flags := make([]byte, 8)
	flags[7-spec.flag/8] = 1 << uint(spec.flag%8)
	p := append(append([]byte(nil), seq...), 1)
	p = append(p, flags...)
	return append(p, values...), nil
}
func flowConnection(ctx context.Context, c net.Conn, key string, flow int) (map[string]any, error) {
	spec, ok := flowModel(key)
	if !ok {
		return nil, errors.New("未知造浪模型")
	}
	return writeModelConnection(ctx, c, key, spec.size, func(b, seq []byte) ([]byte, error) { return flowPayload(key, b, flow, seq) }, func(before, after []byte) error {
		if after[2] != byte(flow) {
			return errors.New("設備讀回強度與設定不符，請查看 App")
		}
		if before[0] != after[0] || before[1] != after[1] {
			return errors.New("模式／聯動旗標發生變化，請查看 App")
		}
		return nil
	})
}
func SetWaveFlow(ctx context.Context, id Identity, flow int) (map[string]any, error) {
	if _, ok := flowModel(id.ProductKey); !ok || flow < 1 || flow > 100 {
		return nil, errors.New("不支援的造浪或強度")
	}
	c, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(id.IP, "12416"))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return flowConnection(ctx, c, id.ProductKey, flow)
}
func (m *Monitor) registerFlow(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/hub/jebao/wavemakers/{id}/flow", func(w http.ResponseWriter, r *http.Request) {
		index := -1
		for i := 1; i < len(m.devices); i++ {
			if m.devices[i].ID == r.PathValue("id") {
				index = i
				break
			}
		}
		if index < 0 {
			reply(w, 404, map[string]string{"error": "未知造浪設備"})
			return
		}
		var in struct {
			Flow *int `json:"flow"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.Flow == nil || *in.Flow < 1 || *in.Flow > 100 {
			reply(w, 400, map[string]string{"error": "強度須為 1–100 的整數"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		expected := "50dbc92221fd4d33ae69a1fedd43b555"
		if index == 3 {
			expected = "54114ccdac1e41c0bb17e222887c07ba"
		}
		attrs, err := m.controlDevice(ctx, index, func(key string) bool { return key == expected }, func(ctx context.Context, id Identity) (map[string]any, error) { return m.writeFlow(ctx, id, *in.Flow) })
		if err != nil {
			reply(w, 409, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, map[string]any{"flow": attrs["Flow"], "verified_local": true, "cloud_verified": false})
	})
}
