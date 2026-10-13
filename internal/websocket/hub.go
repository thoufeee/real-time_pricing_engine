package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 50 * time.Second
	maxMessageSize = 4096
	clientQueue    = 256
)

// Message represents a client subscription request or server response.
type Message struct {
	Type    string   `json:"type"`
	Symbols []string `json:"symbols,omitempty"`
	Message string   `json:"message,omitempty"`
}

// Client represents one connected WebSocket client.
type Client struct {
	conn *websocket.Conn
	send chan any
	subs map[string]bool
	hub  *Hub
}

// Hub manages WebSocket clients and their symbol subscriptions.
type Hub struct {
	mu      sync.Mutex
	clients map[*Client]bool
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]bool),
	}
}

func (h *Hub) addClient(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

// removeClient unregisters a client and safely closes its send channel.
func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()

	if h.clients[c] {
		delete(h.clients, c)
		close(c.send)
	}

	h.mu.Unlock()
}

// Subscribe subscribes a connected client to the given symbols.
func (h *Hub) Subscribe(c *Client, symbols []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.clients[c] {
		return
	}

	for _, symbol := range normalizeSymbols(symbols) {
		c.subs[symbol] = true
	}
}

// Unsubscribe removes the client's symbol subscriptions.
func (h *Hub) Unsubscribe(c *Client, symbols []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.clients[c] {
		return
	}

	for _, symbol := range normalizeSymbols(symbols) {
		delete(c.subs, symbol)
	}
}

// Broadcast sends a price snapshot to clients subscribed to its symbol.
// It does not block market-feed processing if a client is slow.
func (h *Hub) Broadcast(snapshot any, symbol string) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		if !c.subs[symbol] {
			continue
		}

		select {
		case c.send <- snapshot:
		default:
			// Remove the slow client while holding the hub lock.
			delete(h.clients, c)
			close(c.send)

			// Closing the connection interrupts a blocked read.
			go func(client *Client) {
				_ = client.conn.Close()
			}(c)

			log.Printf(
				"Disconnected slow WebSocket client; queue full",
			)
		}
	}
}

// ServeHTTP upgrades an HTTP request to WebSocket and manages the client.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Permit non-browser clients such as Postman.
				return true
			}

			u, err := url.Parse(origin)
			return err == nil &&
				strings.EqualFold(u.Host, r.Host)
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan any, clientQueue),
		subs: make(map[string]bool),
		hub:  h,
	}

	h.addClient(client)

	// The writer goroutine is the only goroutine that writes
	// application messages and ping frames to this connection.
	go client.writePump()

	client.enqueue(Message{
		Type:    "connected",
		Message: "pricing engine WebSocket connected",
	})

	log.Printf("WebSocket client connected: %s", r.RemoteAddr)

	// readPump returns when the client disconnects.
	// Its deferred cleanup unregisters the client.
	client.readPump()
}

// readPump reads subscription and unsubscription requests.
func (c *Client) readPump() {
	defer func() {
		c.hub.removeClient(c)
		_ = c.conn.Close()
		log.Printf("WebSocket client disconnected")
	}()

	c.conn.SetReadLimit(maxMessageSize)

	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(
				err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
			) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		if messageType != websocket.TextMessage {
			c.enqueue(Message{
				Type:    "error",
				Message: "Only JSON text messages are supported",
			})
			continue
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.enqueue(Message{
				Type:    "error",
				Message: "Invalid JSON message",
			})
			continue
		}

		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case "subscribe":
			symbols := normalizeSymbols(msg.Symbols)
			if len(symbols) == 0 {
				c.enqueue(Message{
					Type:    "error",
					Message: "Provide at least one symbol to subscribe",
				})
				continue
			}

			c.hub.Subscribe(c, symbols)
			c.enqueue(Message{
				Type:    "subscribed",
				Symbols: symbols,
			})

			log.Printf("Client subscribed to: %s", strings.Join(symbols, ", "))

		case "unsubscribe":
			symbols := normalizeSymbols(msg.Symbols)
			c.hub.Unsubscribe(c, symbols)

			c.enqueue(Message{
				Type:    "unsubscribed",
				Symbols: symbols,
			})

		default:
			c.enqueue(Message{
				Type:    "error",
				Message: "type must be subscribe or unsubscribe",
			})
		}
	}
}

func normalizeSymbols(symbols []string) []string {
	result := make([]string, 0, len(symbols))
	seen := make(map[string]bool, len(symbols))

	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" || seen[symbol] {
			continue
		}

		seen[symbol] = true
		result = append(result, symbol)
	}

	return result
}

// enqueue queues a message without blocking the calling goroutine.
func (c *Client) enqueue(message any) {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()

	if !c.hub.clients[c] {
		return
	}

	select {
	case c.send <- message:
	default:
		delete(c.hub.clients, c)
		close(c.send)

		go func() {
			_ = c.conn.Close()
		}()

		log.Printf("Disconnected slow WebSocket client; queue full")
	}
}

// writePump is the sole writer for each WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))

			if !ok {
				_ = c.conn.WriteMessage(
					websocket.CloseMessage,
					[]byte{},
				)
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))

			if err := c.conn.WriteMessage(
				websocket.PingMessage,
				nil,
			); err != nil {
				return
			}
		}
	}
}
