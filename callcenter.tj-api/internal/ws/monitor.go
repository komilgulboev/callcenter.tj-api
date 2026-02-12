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
	CheckOrigin: func(r *http.Request) bool { return true },
}

type snapshot struct {
	Type   string                         `json:"type"`
	Agents map[string]monitor.AgentState `json:"agents"`
	Calls  map[string]monitor.Call       `json:"calls"`
	Queues map[string]monitor.QueueStats `json:"queues"`
}

func Monitor(
	agentStore *monitor.Store,
	callStore *monitor.CallStore,
	queueStore *monitor.QueueStore,
	cfg *config.Config,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

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

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		log.Printf("🟢 WS connected | tenant=%d", tenantID)

		writeSnapshot := func() error {
			snap := snapshot{
				Type:   "snapshot",
				Agents: agentStore.GetAgents(tenantID),
				Calls:  callStore.GetCalls(tenantID),
				Queues: queueStore.Snapshot(tenantID),
			}
			
			log.Printf("📡 WS Snapshot | tenant=%d | agents=%d | calls=%d | queues=%d", 
				tenantID, len(snap.Agents), len(snap.Calls), len(snap.Queues))
			
			// Логируем детали звонков для отладки
			for callID, call := range snap.Calls {
				log.Printf("  📞 Call: id=%s, from=%s, to=%s, channel=%s", 
					callID, call.From, call.To, call.Channel)
			}
			
			return conn.WriteJSON(snap)
		}

		// 🔥 первый снапшот
		if err := writeSnapshot(); err != nil {
			return
		}
		
		// 🔔 subscriptions
		agentCh := make(chan monitor.AgentEvent, 16)
		agentStore.SubscribeAgents(tenantID, agentCh)
		defer agentStore.UnsubscribeAgents(tenantID, agentCh)

		queueCh := make(chan struct{}, 16)
		queueStore.Subscribe(tenantID, queueCh)
		defer queueStore.Unsubscribe(tenantID, queueCh)

		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()

		for {
			select {

			case <-agentCh:
				if err := writeSnapshot(); err != nil {
					return
				}

			case <-queueCh:
				if err := writeSnapshot(); err != nil {
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