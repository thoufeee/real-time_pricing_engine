package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"Real_TimePricingEngine/internal/models"
)

func TestPriceSnapshotPayloadUnder10KB(t *testing.T) {
	snapshot := models.PriceSnapshot{
		Symbol:    "BTCUSD",
		Buy:       83396.31,
		Sell:      83519.21,
		High24h:   83616.365,
		Low24h:    83114.695,
		Change24h: 0.1896,
		Timestamp: time.Now().UTC(),
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("failed to marshal snapshot: %v", err)
	}

	size := len(payload)
	t.Logf("Serialized PriceSnapshot size: %d bytes", size)

	if size >= 10_000 {
		t.Fatalf("payload must be under 10000 bytes; got %d", size)
	}
}
