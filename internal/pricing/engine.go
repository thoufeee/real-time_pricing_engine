package pricing

import (
	"Real_TimePricingEngine/internal/models"
	"bytes"
	"container/heap"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const rollingWindow = 24 * time.Hour

type sample struct {
	timestamp time.Time
	price     float64
	key       string
}

type priceHeap struct {
	items []*sample
	isMin bool
}

func (h priceHeap) Len() int {
	return len(h.items)
}

func (h priceHeap) Less(i, j int) bool {
	if h.items[i].price == h.items[j].price {
		return h.items[i].timestamp.Before(h.items[j].timestamp)
	}

	if h.isMin {
		return h.items[i].price < h.items[j].price
	}

	return h.items[i].price > h.items[j].price
}

func (h priceHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
}

func (h *priceHeap) Push(value any) {
	h.items = append(h.items, value.(*sample))
}

func (h *priceHeap) Pop() any {
	old := h.items
	n := len(old)
	item := old[n-1]
	h.items = old[:n-1]
	return item
}

type symbolState struct {
	history []*sample
	seen    map[string]struct{}

	minHeap priceHeap
	maxHeap priceHeap

	latest  *models.FeedTick
	maxSeen time.Time
}

type Engine struct {
	mu      sync.RWMutex
	symbols map[string]*symbolState
}

func NewEngine() *Engine {
	return &Engine{
		symbols: make(map[string]*symbolState),
	}
}

func newSymbolState() *symbolState {
	state := &symbolState{
		seen: make(map[string]struct{}),
		minHeap: priceHeap{
			isMin: true,
		},
		maxHeap: priceHeap{
			isMin: false,
		},
	}

	heap.Init(&state.minHeap)
	heap.Init(&state.maxHeap)

	return state
}

// It returns one snapshot for each accepted tick.
func (e *Engine) Process(payload []byte) ([]models.PriceSnapshot, error) {
	ticks, err := decodeTicks(payload)
	if err != nil {
		return nil, err
	}

	snapshots := make([]models.PriceSnapshot, 0, len(ticks))

	for _, tick := range ticks {
		snapshot, accepted, err := e.processTick(tick)
		if err != nil {
			// Skip this tick and continue processing other symbols.
			fmt.Printf("Skipping invalid market tick: %v\n", err)
			continue
		}
		if accepted {
			snapshots = append(snapshots, snapshot)
		}
	}

	return snapshots, nil
}

func decodeTicks(payload []byte) ([]models.FeedTick, error) {
	payload = bytes.TrimSpace(payload)

	if len(payload) == 0 {
		return nil, fmt.Errorf("empty market-feed payload")
	}

	// Handle arrays of ticks.
	if payload[0] == '[' {
		var ticks []models.FeedTick
		if err := json.Unmarshal(payload, &ticks); err != nil {
			return nil, fmt.Errorf("decode market ticks: %w", err)
		}
		return ticks, nil
	}

	// Handle an object, optionally wrapped in "data"
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, fmt.Errorf("decode market message: %w", err)
	}

	if data, ok := object["data"]; ok {
		if len(bytes.TrimSpace(data)) == 0 ||
			bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return nil, fmt.Errorf("market message contains empty data")
		}

		return decodeTicks(data)
	}

	var tick models.FeedTick
	if err := json.Unmarshal(payload, &tick); err != nil {
		return nil, fmt.Errorf("decode market tick: %w", err)
	}

	return []models.FeedTick{tick}, nil
}

func makeSampleKey(tick models.FeedTick) string {
	return fmt.Sprintf(
		"%s|%d|%s|%s",
		strings.ToUpper(strings.TrimSpace(tick.Symbol)),
		tick.Timestamp.UnixNano(),
		strconvPrice(tick.Buy),
		strconvPrice(tick.Sell),
	)
}

func strconvPrice(value float64) string {
	return fmt.Sprintf("%.12g", value)
}

func (e *Engine) processTick(
	tick models.FeedTick,
) (models.PriceSnapshot, bool, error) {
	tick.Symbol = strings.ToUpper(strings.TrimSpace(tick.Symbol))

	if tick.Symbol == "" {
		return models.PriceSnapshot{}, false, fmt.Errorf("symbol is required")
	}

	if tick.Buy <= 0 || tick.Sell <= 0 ||
		math.IsNaN(tick.Buy) || math.IsNaN(tick.Sell) ||
		math.IsInf(tick.Buy, 0) || math.IsInf(tick.Sell, 0) {
		return models.PriceSnapshot{}, false, fmt.Errorf(
			"invalid buy/sell price for %s",
			tick.Symbol,
		)
	}

	if tick.Timestamp.IsZero() {
		return models.PriceSnapshot{}, false, fmt.Errorf(
			"timestamp is required for %s",
			tick.Symbol,
		)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	state, exists := e.symbols[tick.Symbol]
	if !exists {
		state = newSymbolState()
		e.symbols[tick.Symbol] = state
	}

	timestamp := tick.Timestamp.UTC()

	if timestamp.After(state.maxSeen) {
		state.maxSeen = timestamp
	}

	cutoff := state.maxSeen.Add(-rollingWindow)

	// Remove expired samples from the chronological history
	firstValid := sort.Search(len(state.history), func(i int) bool {
		return !state.history[i].timestamp.Before(cutoff)
	})

	for _, old := range state.history[:firstValid] {
		delete(state.seen, old.key)
	}

	state.history = state.history[firstValid:]

	// Remove expired heap entries lazily
	for state.minHeap.Len() > 0 &&
		state.minHeap.items[0].timestamp.Before(cutoff) {
		heap.Pop(&state.minHeap)
	}

	for state.maxHeap.Len() > 0 &&
		state.maxHeap.items[0].timestamp.Before(cutoff) {
		heap.Pop(&state.maxHeap)
	}

	// A tick outside the current window is too old to affect current stats.
	if timestamp.Before(cutoff) {
		return models.PriceSnapshot{}, false, nil
	}

	key := makeSampleKey(tick)

	// Do not count an identical tick twice
	if _, duplicate := state.seen[key]; duplicate {
		return models.PriceSnapshot{}, false, nil
	}

	midpoint := (tick.Buy + tick.Sell) / 2
	currentSample := &sample{
		timestamp: timestamp,
		price:     midpoint,
		key:       key,
	}

	// Insert by timestamp so out-of-order ticks still contribute to
	// the rolling window correctly.
	index := sort.Search(len(state.history), func(i int) bool {
		return state.history[i].timestamp.After(timestamp)
	})

	state.history = append(state.history, nil)
	copy(state.history[index+1:], state.history[index:])
	state.history[index] = currentSample

	state.seen[key] = struct{}{}

	heap.Push(&state.minHeap, currentSample)
	heap.Push(&state.maxHeap, currentSample)

	// Only a tick at least as recent as the current latest tick can
	// replace the current buy/sell quote
	if state.latest == nil ||
		!timestamp.Before(state.latest.Timestamp.Time) {
		latestTick := tick
		latestTick.Timestamp.Time = timestamp
		state.latest = &latestTick
	}

	// Expire any heap roots made stale by the current cutoff
	for state.minHeap.Len() > 0 &&
		state.minHeap.items[0].timestamp.Before(cutoff) {
		heap.Pop(&state.minHeap)
	}

	for state.maxHeap.Len() > 0 &&
		state.maxHeap.items[0].timestamp.Before(cutoff) {
		heap.Pop(&state.maxHeap)
	}

	if len(state.history) == 0 ||
		state.minHeap.Len() == 0 ||
		state.maxHeap.Len() == 0 ||
		state.latest == nil {
		return models.PriceSnapshot{}, false, nil
	}

	firstPrice := state.history[0].price
	latestPrice := (state.latest.Buy + state.latest.Sell) / 2

	var change float64
	if firstPrice != 0 {
		change = ((latestPrice - firstPrice) / firstPrice) * 100
	}

	snapshot := models.PriceSnapshot{
		Symbol:    tick.Symbol,
		Buy:       state.latest.Buy,
		Sell:      state.latest.Sell,
		High24h:   state.maxHeap.items[0].price,
		Low24h:    state.minHeap.items[0].price,
		Change24h: change,
		Timestamp: state.latest.Timestamp.UTC(),
	}

	return snapshot, true, nil
}

// Get returns the latest calculated snapshot for a symbol
func (e *Engine) Get(symbol string) (models.PriceSnapshot, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	e.mu.RLock()
	defer e.mu.RUnlock()

	state, exists := e.symbols[symbol]
	if !exists || state.latest == nil ||
		len(state.history) == 0 ||
		state.minHeap.Len() == 0 ||
		state.maxHeap.Len() == 0 {
		return models.PriceSnapshot{}, false
	}

	firstPrice := state.history[0].price
	latestPrice := (state.latest.Buy + state.latest.Sell) / 2

	var change float64
	if firstPrice != 0 {
		change = ((latestPrice - firstPrice) / firstPrice) * 100
	}

	return models.PriceSnapshot{
		Symbol:    symbol,
		Buy:       state.latest.Buy,
		Sell:      state.latest.Sell,
		High24h:   state.maxHeap.items[0].price,
		Low24h:    state.minHeap.items[0].price,
		Change24h: change,
		Timestamp: state.latest.Timestamp.UTC(),
	}, true
}

func (e *Engine) Restore(
	snapshots []models.PriceSnapshot,
) error {
	// LoadHistory returns records in chronological order
	for _, snapshot := range snapshots {
		tick := models.FeedTick{
			Symbol: snapshot.Symbol,
			Buy:    snapshot.Buy,
			Sell:   snapshot.Sell,
			Timestamp: models.FlexibleTime{
				Time: snapshot.Timestamp.UTC(),
			},
		}

		_, _, err := e.processTick(tick)
		if err != nil {
			return fmt.Errorf(
				"restore %s at %s: %w",
				snapshot.Symbol,
				snapshot.Timestamp.Format(time.RFC3339Nano),
				err,
			)
		}
	}

	return nil
}
