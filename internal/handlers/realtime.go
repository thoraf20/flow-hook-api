package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSE connection manager
type sseManager struct {
	connections map[string]map[*sseConnection]bool
	mu          sync.RWMutex
}

type sseConnection struct {
	endpointID string
	ch         chan []byte
}

var sseMgr = &sseManager{
	connections: make(map[string]map[*sseConnection]bool),
}

// RealtimeHandler handles GET /api/v1/realtime?endpoint=:slug
func RealtimeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get endpoint slug from query parameter
	slug := r.URL.Query().Get("endpoint")
	if slug == "" {
		http.Error(w, "endpoint parameter is required", http.StatusBadRequest)
		return
	}

	// Get endpoint ID (we'll use slug as the key for simplicity)
	endpointKey := slug

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	// Create connection
	conn := &sseConnection{
		endpointID: endpointKey,
		ch:         make(chan []byte, 10),
	}

	// Register connection
	sseMgr.mu.Lock()
	if sseMgr.connections[endpointKey] == nil {
		sseMgr.connections[endpointKey] = make(map[*sseConnection]bool)
	}
	sseMgr.connections[endpointKey][conn] = true
	sseMgr.mu.Unlock()

	// Send initial connection message
	fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected"}`)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Handle client disconnect
	ctx := r.Context()
	done := ctx.Done()

	// Listen for events
	for {
		select {
		case <-done:
			// Client disconnected
			sseMgr.mu.Lock()
			delete(sseMgr.connections[endpointKey], conn)
			if len(sseMgr.connections[endpointKey]) == 0 {
				delete(sseMgr.connections, endpointKey)
			}
			sseMgr.mu.Unlock()
			close(conn.ch)
			return

		case data := <-conn.ch:
			// Send event to client
			fmt.Fprintf(w, "data: %s\n\n", data)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

// broadcastToSSE sends an event to all SSE connections for an endpoint
func broadcastToSSE(endpointKey string, event interface{}) {
	sseMgr.mu.RLock()
	connections := sseMgr.connections[endpointKey]
	sseMgr.mu.RUnlock()

	if connections == nil || len(connections) == 0 {
		return
	}

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	// Send to all connections (non-blocking)
	sseMgr.mu.RLock()
	for conn := range connections {
		select {
		case conn.ch <- data:
		default:
			// Channel full, skip
		}
	}
	sseMgr.mu.RUnlock()
}

