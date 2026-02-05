package ws

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"callcentrix/internal/auth"
	"callcentrix/internal/config"
	"callcentrix/internal/monitor"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func Monitor(
	agents *monitor.Store,
	calls *monitor.CallStore,
	cfg *config.Config,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		// =======================
		// JWT FROM QUERY
		// =======================
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "token required", http.StatusUnauthorized)
			return
		}

		ctx, err := auth.ParseJWT(token, cfg.JWT.Secret)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		tenantID := ctx.TenantID

		// =======================
		// WS UPGRADE
		// =======================
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		log.Println("🔌 WS CONNECT tenant:", tenantID)

		// =======================
		// SNAPSHOT
		// =======================
		conn.WriteJSON(map[string]any{
			"type": "snapshot",
			"data": map[string]any{
				"agents": agents.Snapshot(tenantID),
				"calls":  calls.Snapshot(tenantID),
			},
		})

		// =======================
		// SUBSCRIBE
		// =======================
		agentSub := agents.Subscribe(tenantID)
		callSub := calls.Subscribe(tenantID)

		defer agents.Unsubscribe(tenantID, agentSub)
		defer calls.Unsubscribe(tenantID, callSub)

		// =======================
		// LOOP
		// =======================
		for {
			select {
			case a := <-agentSub:
				conn.WriteJSON(map[string]any{
					"type": "agent_update",
					"data": a,
				})

			case c := <-callSub:
				conn.WriteJSON(map[string]any{
					"type": "call_update",
					"data": c,
				})
			}
		}
	}
}
