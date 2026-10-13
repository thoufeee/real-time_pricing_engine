package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	URL       string
	APIKey    string
	SecretKey string
}

type Client struct {
	cfg Config
}

type AuthMessage struct {
	Action string `json:"action"`
	APIKey string `json:"api_key"`
	Secret string `json:"secret"`
}

type SubscribeMessage struct {
	Action  string   `json:"action"`
	Symbols []string `json:"symbols"`
}

var Symbols = []string{
	"AUDUSD", "AUDCAD", "AUDCHF", "AUDJPY", "AUDNZD",
	"AUDSGD", "CADCHF", "CADJPY", "CHFJPY", "CHFSGD",
	"EURAUD", "EURCAD", "EURCHF", "EURGBP", "EURJPY",
	"EURNZD", "EURUSD", "GBPAUD", "GBPCAD", "GBPCHF",
	"GBPJPY", "GBPNZD", "GBPUSD", "NZDCAD", "NZDCHF",
	"NZDJPY", "NZDUSD", "USDCAD", "USDCHF", "USDCNH",
	"USDJPY", "USDMXN", "USDNOK", "USDPLN", "USDSEK",
	"USDSGD", "USDTRY", "USDZAR", "EURTRY", "GBPTRY",
	"NOKJPY", "SEKJPY", "SGDJPY", "ZARJPY", "XAUUSD",
	"XAGUSD", "BTCUSD", "ETHUSD", "BNBUSD", "SOLUSD",
	"XRPUSD", "ADAUSD", "DOGUSD", "DOTUSD", "LTCUSD",
	"BCHUSD", "XLMUSD", "TRXUSD", "UNIUSD", "FILUSD",
	"AVXUSD",
}

func NewClient(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme != "wss" || u.Host == "" {
		return nil, errors.New("a valid websocket url is required")
	}

	if strings.TrimSpace(cfg.APIKey) == "" ||
		strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, errors.New("API key and secret key are required")
	}

	return &Client{cfg: cfg}, nil
}

// Run connects to the provider, authenticates, subscribes to market prices,
// and forwards raw messages to onMessage.
func (c *Client) Run(
	ctx context.Context,
	onMessage func([]byte),
) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("connect to market feed: %w", err)
	}
	defer func() {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		_ = conn.Close()
	}()

	// Authenticate.
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	auth := AuthMessage{
		Action: "auth",
		APIKey: strings.TrimSpace(c.cfg.APIKey),
		Secret: strings.TrimSpace(c.cfg.SecretKey),
	}

	if err := conn.WriteJSON(auth); err != nil {
		return fmt.Errorf("send authentication: %w", err)
	}

	authenticated := false

	for !authenticated {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read authentication response: %w", err)
		}

		var response map[string]any
		if err := json.Unmarshal(message, &response); err != nil {
			return fmt.Errorf("invalid authentication response: %w", err)
		}

		if isAuthenticationSuccess(response) {
			authenticated = true
			break
		}

		if isAuthenticationFailure(response) {
			return errors.New("provider rejected authentication")
		}
	}

	// Subscribe to market prices.
	subscribe := SubscribeMessage{
		Action:  "subscribeMarketPrice",
		Symbols: Symbols,
	}

	if err := conn.WriteJSON(subscribe); err != nil {
		return fmt.Errorf("subscribe to market prices: %w", err)
	}

	// TEMPORARY DEBUG: print the first three incoming market messages.
	// Remove this diagnostic after confirming the provider's message schema.
	loggedSamples := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		_, message, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read market feed: %w", err)
		}

		if onMessage == nil {
			continue
		}

		if loggedSamples < 3 {
			sample := string(message)
			if len(sample) > 2000 {
				sample = sample[:2000] + "... [truncated]"
			}

			fmt.Printf(
				"Market feed sample %d: %s\n",
				loggedSamples+1,
				sample,
			)
			loggedSamples++
		}

		onMessage(message)
	}
}

// These helpers recognize common authentication response formats.
func isAuthenticationSuccess(response map[string]any) bool {
	for _, key := range []string{"status", "message", "event"} {
		value, ok := response[key].(string)
		if !ok {
			continue
		}

		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "authenticated", "authentication_success", "auth_success":
			return true
		}
	}

	return false
}

func isAuthenticationFailure(response map[string]any) bool {
	for _, key := range []string{"status", "message", "event"} {
		value, ok := response[key].(string)
		if !ok {
			continue
		}

		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "unauthorized") ||
			strings.Contains(value, "authentication_failed") ||
			strings.Contains(value, "auth_failed") {
			return true
		}
	}

	return false
}
