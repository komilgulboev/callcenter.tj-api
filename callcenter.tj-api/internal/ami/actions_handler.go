package ami

import (
	"encoding/json"
	"log"
	"net/http"

	"callcentrix/internal/auth"
	"callcentrix/internal/monitor"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ActionsHandler struct {
	DB     *pgxpool.Pool
	AMI    *Service
	Calls  *monitor.CallStore
	Agents *monitor.Store
}

// =========================
// PAUSE / UNPAUSE
// =========================

func (h *ActionsHandler) TogglePause(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		http.Error(w, "missing agent", http.StatusBadRequest)
		return
	}

	_, err := h.DB.Exec(
		r.Context(),
		`
		UPDATE queue_members
		SET paused = CASE WHEN paused = 1 THEN 0 ELSE 1 END
		WHERE interface = $1
		`,
		"PJSIP/"+agent,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Hangup godoc
// @Summary      Hangup call
// @Description  Force hangup call by callId (Linkedid)
// @Tags         Actions
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        callId query string true "Call Linkedid"
// @Success      200 {string} string "ok"
// @Failure      400 {string} string "missing callId"
// @Failure      401 {string} string "unauthorized"
// @Failure      404 {string} string "call not found"
// @Failure      500 {string} string "AMI not available"
// @Router       /api/actions/hangup [post]
func (h *ActionsHandler) Hangup(w http.ResponseWriter, r *http.Request) {
	callID := r.URL.Query().Get("callId")
	if callID == "" {
		http.Error(w, "missing callId", http.StatusBadRequest)
		return
	}

	if h.AMI == nil {
		http.Error(w, "AMI not available", http.StatusInternalServerError)
		return
	}

	// 🔑 tenant берём ИЗ КОНТЕКСТА
	user := auth.FromContext(r.Context())
	tenantID := user.TenantID

	log.Printf("🔍 Hangup request: callID=%s, tenantID=%d, user=%+v", callID, tenantID, user)

	// 🔎 ищем call в CallStore
	calls := h.Calls.GetCalls(tenantID)
	
	log.Printf("📊 Available calls in CallStore for tenantID=%d: %d", tenantID, len(calls))
	for id, c := range calls {
		log.Printf("  - Call: id=%s, from=%s, to=%s, channel=%s", id, c.From, c.To, c.Channel)
	}

	// 🔍 Также проверим агентов
	agents := h.Agents.GetAgents(tenantID)
	log.Printf("👥 Available agents for tenantID=%d: %d", tenantID, len(agents))
	for name, agent := range agents {
		log.Printf("  - Agent: name=%s, status=%s, callID=%s", name, agent.Status, agent.CallID)
	}

	call, ok := calls[callID]
	
	// Если звонок не найден в CallStore, пытаемся найти через агентов
	if !ok {
		log.Printf("⚠️ Call not found in CallStore, searching via agents...")
		
		for name, agent := range agents {
			if agent.CallID == callID {
				log.Printf("✅ Found agent with callID: agent=%s, status=%s", name, agent.Status)
				
				// Пытаемся получить channel из call если есть
				if c, found := calls[callID]; found {
					call = c
					ok = true
					log.Printf("✅ Call found via agent search")
					break
				}
				
				// Если не нашли call, но агент имеет этот callID
				log.Printf("⚠️ Agent has callID but call not in CallStore. Checking ALL tenants...")
				
				// 🔥 ПОИСК во всех tenant'ах
				allTenantIDs := []int{1, 2, 3, 4, 5, 110001, 110002, 110003} // расширенный список
				for _, tid := range allTenantIDs {
					if tid == tenantID {
						continue // уже проверили
					}
					otherCalls := h.Calls.GetCalls(tid)
					log.Printf("🔎 Checking tenant %d: %d calls", tid, len(otherCalls))
					for id, c := range otherCalls {
						log.Printf("    - Call in tenant %d: id=%s, from=%s, to=%s, channel=%s", tid, id, c.From, c.To, c.Channel)
					}
					
					if c, found := otherCalls[callID]; found {
						log.Printf("🔍 FOUND in different tenant! tenantID=%d, from=%s, to=%s, channel=%s, channels=%v", 
							tid, c.From, c.To, c.Channel, c.Channels)
						// 🎯 ИСПОЛЬЗУЕМ найденный звонок!
						call = c
						ok = true
						log.Printf("✅ Using call from tenant %d", tid)
						break
					}
				}
				
				// Если так и не нашли - только тогда ошибка
				if !ok {
					log.Printf("❌ Call NOT FOUND in ANY tenant! callID=%s", callID)
					http.Error(w, "call details not available", http.StatusNotFound)
					return
				}
				break
			}
		}
	}
	
	if !ok {
		log.Printf("❌ Call not found: callID=%s, tenantID=%d", callID, tenantID)
		http.Error(w, "call not found", http.StatusNotFound)
		return
	}

	log.Printf("✅ Call found: channel=%s, channels=%v", call.Channel, call.Channels)

	// Используем первый доступный канал
	channelToHangup := call.Channel
	if channelToHangup == "" && len(call.Channels) > 0 {
		channelToHangup = call.Channels[0]
	}
	
	if channelToHangup == "" {
		log.Printf("❌ No channel available for hangup")
		http.Error(w, "no channel available", http.StatusInternalServerError)
		return
	}

	// 📡 отправляем Hangup в Asterisk
	err := h.AMI.SendAction("Hangup", map[string]string{
		"Channel": channelToHangup,
	})
	
	if err != nil {
		log.Printf("❌ AMI Hangup error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Hangup sent to Asterisk for channel=%s", channelToHangup)
	
	// 🔄 Сбрасываем агентов немедленно (не ждём AMI Hangup)
	agentsToReset := h.Agents.GetAgents(tenantID)
	for _, a := range agentsToReset {
		if a.CallID == callID {
			log.Printf("🔄 Resetting agent: %s (was: %s, callID: %s)", a.Name, a.Status, a.CallID)
			h.Agents.UpdateAgent(tenantID, monitor.AgentState{
				Name:   a.Name,
				Status: "idle",
				CallID: "",
			})
		}
	}
	
	// 🗑️ Удаляем звонок из CallStore
	h.Calls.RemoveCall(tenantID, callID)
	log.Printf("🗑️ Call removed from CallStore: callID=%s", callID)
	
	w.WriteHeader(http.StatusOK)
}
// GetMyActiveCall godoc
// @Summary      Активный звонок текущего агента
// @Description  Username из JWT = SIP номер агента. Смотрит AgentStore/CallStore (данные из AMI, без БД)
// @Tags         Actions
// @Security     BearerAuth
// @Produce      json
// @Success      200 {object} map[string]any
// @Router       /api/actions/my-call [get]
func (h *ActionsHandler) GetMyActiveCall(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	// Username в JWT — это SIP номер агента (например "1001")
	sipUsername := user.Username

	w.Header().Set("Content-Type", "application/json")

	agent, ok := h.Agents.GetAgents(user.TenantID)[sipUsername]
	if !ok || (agent.Status != "in-call" && agent.Status != "ringing") {
		w.Write([]byte(`{"active":false}`))
		return
	}

	callFrom := ""
	if agent.CallID != "" {
		if call, ok := h.Calls.GetCalls(user.TenantID)[agent.CallID]; ok {
			callFrom = call.From
			if callFrom == sipUsername {
				callFrom = call.To
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"active":   true,
		"callFrom": callFrom,
		"callId":   agent.CallID,
		"status":   agent.Status,
	})
}