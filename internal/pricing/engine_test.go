package pricing

import (
	"Real_TimePricingEngine/internal/models"
	"encoding/json"
	"math"
	"testing"
	"time"
)

func makePayload(t *testing.T, timestamp time.Time, buy, sell float64) []byte {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"symbol":     "EURUSD",
		"buy":        buy,
		"sell":       sell,
		"timestamp":  timestamp.Format(time.RFC3339Nano),
		"low_24h":    0.1, // Intentionally incorrect provider value
		"high_24h":   999.0,
		"change_24h": 500.0,
	})

	if err != nil {
		t.Fatal(err)
	}

	return payload
}

func TestEngineCalculatesOwnStats(t *testing.T) {
	engine := NewEngine()
	start := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

	first := makePayload(t, start, 1.10, 1.12)
	second := makePayload(t, start.Add(time.Hour), 1.20, 1.22)

	if _, err := engine.Process(first); err != nil {
		t.Fatal(err)
	}

	snapshots, err := engine.Process(second)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}

	got := snapshots[0]

	// Midpoints: 1.11 and 1.21.
	if math.Abs(got.High24h-1.21) > 1e-9 {
		t.Errorf("high_24h: got %f, want 1.21", got.High24h)
	}

	if math.Abs(got.Low24h-1.11) > 1e-9 {
		t.Errorf("low_24h: got %f, want 1.11", got.Low24h)
	}

	expectedChange := ((1.21 - 1.11) / 1.11) * 100
	if math.Abs(got.Change24h-expectedChange) > 1e-9 {
		t.Errorf(
			"change_24h: got %f, want %f",
			got.Change24h,
			expectedChange,
		)
	}

	if got.Buy != 1.20 || got.Sell != 1.22 {
		t.Errorf("latest buy/sell not preserved: %+v", got)
	}
}

func TestEngineIgnoresDuplicateTick(t *testing.T) {
	engine := NewEngine()
	now := time.Now().UTC().Truncate(time.Second)
	payload := makePayload(t, now, 1.10, 1.12)

	first, err := engine.Process(payload)
	if err != nil {
		t.Fatal(err)
	}

	second, err := engine.Process(payload)
	if err != nil {
		t.Fatal(err)
	}

	if len(first) != 1 {
		t.Fatalf("expected first tick to be accepted, got %d", len(first))
	}

	if len(second) != 0 {
		t.Fatalf("expected duplicate to be ignored, got %d snapshots", len(second))
	}
}

func TestOutOfOrderTickDoesNotReplaceLatest(t *testing.T) {
	engine := NewEngine()
	start := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

	newer := makePayload(t, start.Add(time.Hour), 1.20, 1.22)
	older := makePayload(t, start, 1.10, 1.12)

	if _, err := engine.Process(newer); err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Process(older); err != nil {
		t.Fatal(err)
	}

	got, ok := engine.Get("EURUSD")
	if !ok {
		t.Fatal("expected EURUSD snapshot")
	}

	if got.Buy != 1.20 || got.Sell != 1.22 {
		t.Errorf("older tick replaced latest quote: %+v", got)
	}

	if math.Abs(got.Low24h-1.11) > 1e-9 {
		t.Errorf("older tick should contribute to low_24h; got %f", got.Low24h)
	}
}

func TestDecodeArrayOfTicks(t *testing.T) {
	engine := NewEngine()
	now := time.Now().UTC()

	payload := []byte(`[
		{"symbol":"EURUSD","buy":1.1,"sell":1.2,"timestamp":"` +
		now.Format(time.RFC3339Nano) + `"},
		{"symbol":"GBPUSD","buy":1.3,"sell":1.4,"timestamp":"` +
		now.Format(time.RFC3339Nano) + `"}
	]`)

	snapshots, err := engine.Process(payload)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}

	_ = models.PriceSnapshot{} // Keeps this test file's imports explicit
}
