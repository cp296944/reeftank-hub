package bootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var noncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type MaintenanceRequest struct {
	Schema    int       `json:"schema"`
	Action    string    `json:"action"`
	Nonce     string    `json:"nonce"`
	CreatedAt time.Time `json:"created_at"`
}

type MaintenanceResult struct {
	Schema      int       `json:"schema"`
	Action      string    `json:"action"`
	Nonce       string    `json:"nonce"`
	CompletedAt time.Time `json:"completed_at"`
	OK          bool      `json:"ok"`
	Version     string    `json:"version"`
}

type MaintenanceOptions struct {
	InstallRoot string
	RequestPath string
	Version     string
	Now         func() time.Time
}

// ApplyMaintenance validates and consumes one structured request. The first
// bootstrap schema intentionally supports healthcheck only; future privileged
// migrations must add explicit action types and tests instead of a shell field.
func ApplyMaintenance(o MaintenanceOptions) (MaintenanceResult, error) {
	if o.InstallRoot == "" {
		o.InstallRoot = "/opt/reeftank-hub"
	}
	if o.RequestPath == "" {
		o.RequestPath = filepath.Join(o.InstallRoot, "state", "maintenance-request.json")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	wantPath := filepath.Clean(filepath.Join(o.InstallRoot, "state", "maintenance-request.json"))
	if filepath.Clean(o.RequestPath) != wantPath {
		return MaintenanceResult{}, errors.New("maintenance request path is not the fixed state path")
	}
	f, err := os.Open(o.RequestPath)
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("open maintenance request: %w", err)
	}
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	_ = f.Close()
	if err != nil {
		return MaintenanceResult{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var req MaintenanceRequest
	if err := dec.Decode(&req); err != nil {
		return MaintenanceResult{}, fmt.Errorf("decode maintenance request: %w", err)
	}
	if req.Schema != 1 {
		return MaintenanceResult{}, fmt.Errorf("unsupported maintenance schema %d", req.Schema)
	}
	if req.Action != "healthcheck" {
		return MaintenanceResult{}, fmt.Errorf("unsupported maintenance action %q", req.Action)
	}
	if !noncePattern.MatchString(req.Nonce) {
		return MaintenanceResult{}, errors.New("maintenance nonce is invalid")
	}
	now := o.Now().UTC()
	if req.CreatedAt.IsZero() || req.CreatedAt.Before(now.Add(-10*time.Minute)) || req.CreatedAt.After(now.Add(time.Minute)) {
		return MaintenanceResult{}, errors.New("maintenance request is expired or from the future")
	}
	result := MaintenanceResult{
		Schema: 1, Action: req.Action, Nonce: req.Nonce, CompletedAt: now,
		OK: true, Version: o.Version,
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	resultPath := filepath.Join(o.InstallRoot, "state", "maintenance-result.json")
	if err := writeAtomic(resultPath, append(out, '\n'), 0o644); err != nil {
		return MaintenanceResult{}, err
	}
	if err := os.Remove(o.RequestPath); err != nil {
		return MaintenanceResult{}, fmt.Errorf("consume maintenance request: %w", err)
	}
	return result, nil
}
