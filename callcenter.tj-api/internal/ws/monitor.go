package ws

import (
	"log"
	"net/http"
	"time"

	"callcentrix/internal/auth"
	"callcentrix/internal/config"
	"callcentrix/internal/monitor"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type snapshot struct {
	Type   string                         `json:"type"`
	Agents map[string]monitor.AgentState `json:"agents"`
	Calls  map[string]monitor.Call       `json:"calls"`
}

func Monitor(
	agentStore *monitor.Store,
	callStore *monitor.CallStore,
	cfg *config.Config,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		// =========================
		// AUTH (JWT via query)
		// =========================
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		user, err := auth.ParseJWT(token, cfg.JWT.Secret)
		if err != nil || user.TenantID <= 0 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		tenantID := user.TenantID

		// =========================
		// WS UPGRADE
		// =========================
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("ws upgrade error:", err)
			return
		}
		defer func() {
			conn.Close()
			log.Printf("🔴 WS closed | tenant=%d\n", tenantID)
		}()

		log.Printf("🟢 WS connected | tenant=%d\n", tenantID)

		// =========================
		// INITIAL SNAPSHOT
		// =========================
		if err := safeWrite(conn, snapshot{
			Type:   "snapshot",
			Agents: agentStore.GetAgents(tenantID),
			Calls:  callStore.GetCalls(tenantID),
		}); err != nil {
			return
		}

		log.Printf(
			"WS snapshot tenant=%d agents=%d calls=%d\n",
			tenantID,
			len(agentStore.GetAgents(tenantID)),
			len(callStore.GetCalls(tenantID)),
		)

		// =========================
		// SUBSCRIBE
		// =========================
		agentCh := make(chan monitor.AgentEvent, 16)

		agentStore.SubscribeAgents(tenantID, agentCh)
		defer agentStore.UnsubscribeAgents(tenantID, agentCh)


		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()

		// =========================
		// LOOP
		// =========================
		for {
			select {

			case <-agentCh:
				if err := safeWrite(conn, snapshot{
					Type:   "snapshot",
					Agents: agentStore.GetAgents(tenantID),
					Calls:  callStore.GetCalls(tenantID),
				}); err != nil {
					return
				}

			case <-heartbeat.C:
				if err := conn.WriteControl(
					websocket.PingMessage,
					[]byte{},
					time.Now().Add(5*time.Second),
				); err != nil {
					return
				}
			}
		}
	}
}

// =========================
// SAFE WRITE
// =========================

func safeWrite(conn *websocket.Conn, v any) error {
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := conn.WriteJSON(v); err != nil {
		log.Println("ws write error:", err)
		return err
	}
	return nil
}
