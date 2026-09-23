package temperature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type recorder struct{ values []float64 }

func (r *recorder) RecordTemperature(_ context.Context, v float64, _, _ time.Time, _ time.Duration) error {
	r.values = append(r.values, v)
	return nil
}
func (r *recorder) SetSourceStatus(context.Context, string, bool, string, time.Time) error {
	return nil
}
func (r *recorder) TemperatureSource(context.Context) string          { return "direct" }
func (r *recorder) TemperatureInterval(context.Context) time.Duration { return time.Minute }
func (r *recorder) LatestHATemperature(context.Context) (float64, time.Time, bool) {
	return 0, time.Time{}, false
}

func TestPoll(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"datas":[{"WaterTemperature":"25.9"}]}`))
	}))
	defer s.Close()
	rec := &recorder{}
	p := New(s.URL, rec)
	p.poll(context.Background())
	got := p.Snapshot()
	if got.Stale || got.Value != 25.9 || len(rec.values) != 1 {
		t.Fatalf("unexpected reading: %+v values=%v", got, rec.values)
	}
}

func TestUnconfigured(t *testing.T) {
	p := New("", nil)
	if got := p.Snapshot(); got.Configured || !got.Stale {
		t.Fatalf("unexpected: %+v", got)
	}
}
