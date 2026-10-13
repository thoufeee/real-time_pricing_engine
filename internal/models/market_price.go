package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type FlexibleTime struct {
	time.Time
}

func (t *FlexibleTime) UnmarshalJSON(data []byte) error {
	value := strings.TrimSpace(string(data))

	if value == "" || value == "null" || value == `""` {
		t.Time = time.Time{}
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		text = strings.TrimSpace(text)

		if text == "" {
			t.Time = time.Time{}
			return nil
		}

		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			t.Time = parsed.UTC()
			return nil
		}

		value = text
	}

	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp %q", value)
	}

	switch {
	case n >= 1e18 || n <= -1e18:
		t.Time = time.Unix(0, n).UTC()
	case n >= 1e15 || n <= -1e15:
		t.Time = time.UnixMicro(n).UTC()
	case n >= 1e12 || n <= -1e12:
		t.Time = time.UnixMilli(n).UTC()
	default:
		t.Time = time.Unix(n, 0).UTC()
	}

	return nil
}

type FeedTick struct {
	Symbol    string       `json:"symbol"`
	Buy       float64      `json:"buy"`
	Sell      float64      `json:"sell"`
	Timestamp FlexibleTime `json:"timestamp"`

	ProviderLow24h    float64 `json:"low_24h"`
	ProviderHigh24h   float64 `json:"high_24h"`
	ProviderChange24h float64 `json:"change_24h"`
}

// UnmarshalJSON accepts both our normalized fields and the provider's
// actual marketPriceSnapshot fields.
func (t *FeedTick) UnmarshalJSON(data []byte) error {
	var raw struct {
		Symbol    string       `json:"symbol"`
		Buy       float64      `json:"buy"`
		Sell      float64      `json:"sell"`
		Bid       float64      `json:"bid"`
		Ask       float64      `json:"ask"`
		Timestamp FlexibleTime `json:"timestamp"`

		Low24h    float64 `json:"low_24h"`
		High24h   float64 `json:"high_24h"`
		Change24h float64 `json:"change_24h"`

		Low        float64 `json:"low"`
		High       float64 `json:"high"`
		Percentage float64 `json:"percentage"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode market tick: %w", err)
	}

	buy := raw.Buy
	if buy == 0 {
		buy = raw.Bid
	}

	sell := raw.Sell
	if sell == 0 {
		sell = raw.Ask
	}

	low := raw.Low24h
	if low == 0 {
		low = raw.Low
	}

	high := raw.High24h
	if high == 0 {
		high = raw.High
	}

	change := raw.Change24h
	if change == 0 {
		change = raw.Percentage
	}

	t.Symbol = raw.Symbol
	t.Buy = buy
	t.Sell = sell
	t.Timestamp = raw.Timestamp

	t.ProviderLow24h = low
	t.ProviderHigh24h = high
	t.ProviderChange24h = change

	// The provider's sample does not contain a timestamp.
	// Use receive time as a fallback.
	if t.Timestamp.IsZero() {
		t.Timestamp.Time = time.Now().UTC()
	}

	return nil
}

type PriceSnapshot struct {
	Symbol    string    `json:"symbol"`
	Buy       float64   `json:"buy"`
	Sell      float64   `json:"sell"`
	High24h   float64   `json:"high_24h"`
	Low24h    float64   `json:"low_24h"`
	Change24h float64   `json:"change_24h"`
	Timestamp time.Time `json:"timestamp"`
}
