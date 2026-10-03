package jebao

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed models.json THIRD_PARTY_LICENSE.txt
var modelFiles embed.FS

type attribute struct {
	Name     string   `json:"name"`
	DataType string   `json:"data_type"`
	Type     string   `json:"type"`
	Enum     []string `json:"enum"`
	Position struct {
		ByteOffset int    `json:"byte_offset"`
		BitOffset  int    `json:"bit_offset"`
		Length     int    `json:"len"`
		Unit       string `json:"unit"`
	} `json:"position"`
}
type model struct {
	Name  string      `json:"name"`
	Attrs []attribute `json:"attrs"`
}

func loadModel(key string) (model, error) {
	b, _ := modelFiles.ReadFile("models.json")
	var all map[string]model
	if err := json.Unmarshal(b, &all); err != nil {
		return model{}, err
	}
	m, ok := all[key]
	if !ok {
		return model{}, errors.New("unknown product key; model has not been validated")
	}
	return m, nil
}
func (m model) decode(data []byte) (map[string]any, error) {
	need, groupBits := 0, 0
	for _, a := range m.Attrs {
		p := a.Position
		end := p.ByteOffset + p.Length
		if p.Unit == "bit" {
			end = p.ByteOffset + (p.BitOffset+p.Length+7)/8
			if p.ByteOffset == 0 && p.BitOffset+p.Length > groupBits {
				groupBits = p.BitOffset + p.Length
			}
		}
		if end > need {
			need = end
		}
	}
	if need == 0 || len(data) != need {
		return nil, fmt.Errorf("status length %d; model requires %d (unverified format)", len(data), need)
	}
	groupBytes := (groupBits + 7) / 8
	if groupBytes > 8 {
		return nil, errors.New("unsupported packed bit group")
	}
	out := map[string]any{}
	for _, a := range m.Attrs {
		p := a.Position
		if p.ByteOffset < 0 || p.Length <= 0 || p.BitOffset < 0 {
			return nil, errors.New("invalid model position")
		}
		if a.DataType == "binary" {
			continue
		}
		var value uint64
		switch a.DataType {
		case "bool", "enum":
			width := (p.BitOffset + p.Length + 7) / 8
			if p.ByteOffset == 0 {
				width = groupBytes
			}
			if width > 8 || p.Length > 32 {
				return nil, errors.New("unsupported bit width")
			}
			for _, v := range data[p.ByteOffset : p.ByteOffset+width] {
				value = (value << 8) | uint64(v)
			}
			value = (value >> p.BitOffset) & ((uint64(1) << p.Length) - 1)
		case "uint8":
			value = uint64(data[p.ByteOffset])
		case "uint16":
			value = uint64(data[p.ByteOffset])<<8 | uint64(data[p.ByteOffset+1])
		default:
			continue
		}
		if a.DataType == "bool" {
			out[a.Name] = value != 0
		} else {
			out[a.Name] = value
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no decoded status attributes")
	}
	return out, nil
}
