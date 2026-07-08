package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"k2-gateway/internal/auth"
	"k2-gateway/internal/session"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 180 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 16384
)

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	s.handleWebSocketConn(w, r, false)
}

func (s *Server) handlePublicWebSocket(w http.ResponseWriter, r *http.Request) {
	s.handleWebSocketConn(w, r, true)
}

func (s *Server) handleWebSocketConn(w http.ResponseWriter, r *http.Request, publicOnly bool) {
	req := r
	var provisioned *MobileSIPProvisionResult
	if s.tokenVerifier != nil && !publicOnly {
		rawToken := strings.TrimSpace(r.URL.Query().Get("access_token"))
		if rawToken == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		realmHint := extractAuthRealmHint(r)
		claims, err := s.tokenVerifier.VerifyToken(r.Context(), rawToken, realmHint)
		if err != nil {
			log.Printf("WebSocket auth rejected: hint=%s err=%v", realmHint, err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		log.Printf("WebSocket auth accepted: hint=%s realm=%s sub=%s", realmHint, claims.Realm, claims.Subject)
		if s.mobileProvisioner != nil && claims.Realm == auth.TokenRealmUser {
			devicePlatform, ok := normalizeDevicePlatform(r.URL.Query().Get("devicePlatform"))
			if !ok || devicePlatform == "" {
				log.Printf("WebSocket mobile SIP provisioning rejected: sub=%s stage=device_platform", claims.Subject)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			startedAt := time.Now()
			result, err := s.mobileProvisioner.ProvisionMobileSIPTrunk(r.Context(), rawToken, claims, devicePlatform)
			if err != nil {
				log.Printf("WebSocket mobile SIP provisioning rejected: sub=%s stage=provision elapsed=%s err=%v", claims.Subject, time.Since(startedAt).Round(10*time.Millisecond), err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			provisioned = result
			trunkID := int64(0)
			if result != nil {
				trunkID = result.TrunkID
			}
			log.Printf("WebSocket mobile SIP provisioning accepted: sub=%s trunkID=%d elapsed=%s", claims.Subject, trunkID, time.Since(startedAt).Round(10*time.Millisecond))
		}
		req = withAuthClaims(r, claims)
	} else if publicOnly {
		log.Printf("Public WebSocket connection accepted: remote=%s", r.RemoteAddr)
	}

	conn, err := s.upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	client := &WSClient{
		conn:         conn,
		clientID:     uuid.NewString(),
		send:         make(chan []byte, 256),
		availability: clientAvailabilityIdle,
		callState:    string(session.StateNew),
		ConnectedAt:  time.Now(),
		publicOnly:   publicOnly,
	}
	if claims, ok := AuthClaimsFromContext(req.Context()); ok {
		client.authClaims = claims
	}
	if provisioned != nil && provisioned.TrunkID > 0 {
		client.trunkResolved = true
		client.resolvedTrunkID = provisioned.TrunkID
	}
	s.mu.Lock()
	s.wsConnections[client] = struct{}{}
	s.mu.Unlock()

	// Start write pump
	go s.wsWritePump(client)
	if provisioned != nil && provisioned.TrunkID > 0 {
		s.sendWSMessage(client, WSMessage{
			Type:          "trunk_resolved",
			TrunkID:       provisioned.TrunkID,
			TrunkPublicID: provisioned.TrunkPublicID,
		})
	}

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		if s.config.DebugWebSocket {
			fmt.Printf("[WebSocket] 🏓 Received native pong from client (sessionID=%s)\n", client.sessionID)
		}
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Read messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		s.handleWSMessage(client, message)
	}

	// Cleanup - only delete if this client is still the registered one
	s.mu.Lock()
	delete(s.wsConnections, client)
	if client.sessionID != "" {
		if s.wsClients[client.sessionID] == client {
			delete(s.wsClients, client.sessionID)
		}
	}
	s.mu.Unlock()
}

// wsWritePump pumps messages from the send channel to the WebSocket connection
// wsWritePump pumps messages from the send channel to the WebSocket connection
func (s *Server) wsWritePump(client *WSClient) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := client.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

			// Send any queued messages as SEPARATE WebSocket frames
			// (Don't concatenate into single frame - causes JSON parse errors)
			n := len(client.send)
			for i := 0; i < n; i++ {
				queuedMsg := <-client.send
				client.conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := client.conn.WriteMessage(websocket.TextMessage, queuedMsg); err != nil {
					return
				}
			}
		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if s.config.DebugWebSocket {
				fmt.Printf("[WebSocket] 🏓 Sending native ping to client (sessionID=%s)\n", client.sessionID)
			}
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
