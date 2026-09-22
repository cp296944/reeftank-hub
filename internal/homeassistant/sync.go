package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Recorder interface {
	RecordState(context.Context, State, time.Time) error
	SetSourceStatus(context.Context, string, bool, string, time.Time) error
}

type Syncer struct {
	client  *Client
	record  Recorder
	tracked map[string]bool
	mu      sync.RWMutex
	states  map[string]State
	ready   bool
	last    time.Time
}

func NewSyncer(client *Client, record Recorder, entityIDs []string) *Syncer {
	tracked := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		tracked[id] = true
	}
	return &Syncer{client: client, record: record, tracked: tracked, states: map[string]State{}}
}

func (s *Syncer) Snapshot() (map[string]State, bool, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]State, len(s.states))
	for id, state := range s.states {
		out[id] = state
	}
	return out, s.ready, s.last
}

func (s *Syncer) Run(ctx context.Context) {
	if s.client == nil || !s.client.Configured() {
		return
	}
	for ctx.Err() == nil {
		if err := s.runOnce(ctx); err != nil {
			s.setReady(false)
			if s.record != nil {
				_ = s.record.SetSourceStatus(context.Background(), "home_assistant", false, err.Error(), time.Now())
			}
			delay := time.Duration(2+rand.Intn(4)) * time.Second
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}
}

func (s *Syncer) runOnce(ctx context.Context) error {
	states, err := s.client.States(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for id, state := range states {
		if s.tracked[id] {
			s.accept(ctx, state, now)
		}
	}
	s.setReady(true)
	if s.record != nil {
		_ = s.record.SetSourceStatus(ctx, "home_assistant", true, "", now)
	}

	u, err := url.Parse(s.client.baseURL)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/websocket"
	conn, _, err := websocket.Dial(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	var msg map[string]any
	if err := wsjson.Read(ctx, conn, &msg); err != nil || msg["type"] != "auth_required" {
		return errors.New("unexpected Home Assistant WebSocket greeting")
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "auth", "access_token": s.client.token}); err != nil {
		return err
	}
	if err := wsjson.Read(ctx, conn, &msg); err != nil || msg["type"] != "auth_ok" {
		return errors.New("Home Assistant WebSocket authentication failed")
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"id": 1, "type": "subscribe_events", "event_type": "state_changed"}); err != nil {
		return err
	}
	for {
		var raw struct {
			Type  string `json:"type"`
			Event struct {
				Data struct {
					EntityID string `json:"entity_id"`
					NewState *State `json:"new_state"`
				} `json:"data"`
			} `json:"event"`
		}
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			return err
		}
		if raw.Type == "event" && raw.Event.Data.NewState != nil && s.tracked[raw.Event.Data.EntityID] {
			s.accept(ctx, *raw.Event.Data.NewState, time.Now())
		}
	}
}

func (s *Syncer) accept(ctx context.Context, state State, received time.Time) {
	s.mu.Lock()
	s.states[state.EntityID] = state
	s.last = received.UTC()
	s.mu.Unlock()
	if s.record != nil {
		_ = s.record.RecordState(ctx, state, received)
	}
}

func (s *Syncer) setReady(ready bool) {
	s.mu.Lock()
	s.ready = ready
	s.mu.Unlock()
}

func (s *Syncer) Backfill(ctx context.Context, maxDays int) error {
	if maxDays <= 0 {
		maxDays = 3650
	}
	ids := make([]string, 0, len(s.tracked))
	for id := range s.tracked {
		ids = append(ids, id)
	}
	end := time.Now()
	emptyDays := 0
	for day := 0; day < maxDays && emptyDays < 2; day++ {
		start := end.Add(-24 * time.Hour)
		raw, err := s.client.History(ctx, start, end, ids)
		if err != nil {
			return err
		}
		var groups [][]State
		if err := json.Unmarshal(raw, &groups); err != nil {
			return err
		}
		count := 0
		for _, group := range groups {
			for _, state := range group {
				if state.EntityID == "" && len(group) > 0 {
					state.EntityID = group[0].EntityID
				}
				if state.EntityID != "" {
					_ = s.record.RecordState(ctx, state, time.Now())
					count++
				}
			}
		}
		if count == 0 {
			emptyDays++
		} else {
			emptyDays = 0
		}
		end = start
	}
	return nil
}
