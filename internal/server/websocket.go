package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/coder/websocket"

	"github.com/jfoltran/migrator/internal/metrics"
)

// Hub manages WebSocket clients and broadcasts Snapshot updates.
type Hub struct {
	collector *metrics.Collector
	logger    zerolog.Logger

	mu      sync.Mutex
	clients map[*wsClient]struct{}
}

type wsClient struct {
	conn *websocket.Conn
	done chan struct{}
}

func newHub(collector *metrics.Collector, logger zerolog.Logger) *Hub {
	return &Hub{
		collector: collector,
		logger:    logger.With().Str("component", "ws-hub").Logger(),
		clients:   make(map[*wsClient]struct{}),
	}
}

func (h *Hub) start(ctx context.Context) {
	ch := h.collector.Subscribe()
	defer h.collector.Unsubscribe(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-ch:
			if !ok {
				return
			}
			h.broadcast(snap)
		}
	}
}

func (h *Hub) broadcast(snap metrics.Snapshot) {
	data, err := json.Marshal(snap)
	if err != nil {
		h.logger.Err(err).Msg("marshal snapshot for ws")
		return
	}

	h.mu.Lock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, c := range clients {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.conn.Write(ctx, websocket.MessageText, data)
		cancel()
		if err != nil {
			h.remove(c)
		}
	}
}

func (h *Hub) add(c *wsClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.logger.Debug().Int("clients", len(h.clients)).Msg("ws client connected")
}

func (h *Hub) remove(c *wsClient) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		c.conn.Close(websocket.StatusNormalClosure, "")
	}
	h.mu.Unlock()
}

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow cross-origin for dev.
	})
	if err != nil {
		h.logger.Err(err).Msg("ws accept")
		return
	}

	client := &wsClient{conn: conn, done: make(chan struct{})}
	h.add(client)

	// Send initial snapshot immediately.
	snap := h.collector.Snapshot()
	if data, err := json.Marshal(snap); err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		_ = conn.Write(ctx, websocket.MessageText, data)
		cancel()
	}

	// Keep connection alive by reading (client may send pings).
	for {
		_, _, err := conn.Read(r.Context())
		if err != nil {
			h.remove(client)
			return
		}
	}
}
