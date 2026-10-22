package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Hub struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{conns: map[*websocket.Conn]struct{}{}}
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.conns, conn)
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()
	for {
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
	}
}

func (h *Hub) Broadcast(v any) {
	body, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := c.Write(ctx, websocket.MessageText, body)
		cancel()
		if err != nil {
			h.mu.Lock()
			delete(h.conns, c)
			h.mu.Unlock()
			_ = c.Close(websocket.StatusGoingAway, "")
		}
	}
}
