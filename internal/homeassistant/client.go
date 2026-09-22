package homeassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type State struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	LastChanged string         `json:"last_changed,omitempty"`
	LastUpdated string         `json:"last_updated,omitempty"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Configured() bool { return c.baseURL != "" && c.token != "" }

func (c *Client) States(ctx context.Context) (map[string]State, error) {
	if !c.Configured() {
		return nil, errors.New("Home Assistant credentials are not configured")
	}
	var states []State
	if err := c.do(ctx, http.MethodGet, "/api/states", nil, &states); err != nil {
		return nil, err
	}
	out := make(map[string]State, len(states))
	for _, state := range states {
		out[state.EntityID] = state
	}
	return out, nil
}

func (c *Client) SetSwitch(ctx context.Context, entityID string, on bool) error {
	action := "turn_off"
	if on {
		action = "turn_on"
	}
	return c.do(ctx, http.MethodPost, "/api/services/switch/"+action, map[string]string{"entity_id": entityID}, nil)
}

func (c *Client) History(ctx context.Context, start, end time.Time, entityIDs []string) (json.RawMessage, error) {
	if !c.Configured() {
		return nil, errors.New("Home Assistant credentials are not configured")
	}
	q := url.Values{}
	q.Set("end_time", end.UTC().Format(time.RFC3339))
	q.Set("filter_entity_id", strings.Join(entityIDs, ","))
	q.Set("minimal_response", "")
	q.Set("no_attributes", "")
	path := "/api/history/period/" + url.PathEscape(start.UTC().Format(time.RFC3339)) + "?" + q.Encode()
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Home Assistant request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Home Assistant returned HTTP %d", resp.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
