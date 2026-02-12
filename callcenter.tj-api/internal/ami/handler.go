package ami

import (
	"fmt"
	"strings"
"log"
	"callcentrix/internal/monitor"
)

type Handler struct {
	Agents   *monitor.Store
	Calls    *monitor.CallStore
	Queues   *monitor.QueueStore
	Resolver *monitor.TenantResolver
}

func (h *Handler) HandleEvent(ev map[string]string) {

	tenantID := h.Resolver.Resolve(ev)
	if tenantID == 0 {
		return
	}

	switch ev["Event"] {

	// =====================================================
	// QUEUES
	// =====================================================

	case "QueueParams":
		queue := ev["Queue"]
		h.Queues.Update(tenantID, queue, func(q *monitor.QueueStats) {
			q.Completed = atoi(ev["Completed"])
			q.HoldTime = atoi(ev["Holdtime"])
			q.TalkTime = atoi(ev["TalkTime"])
			q.SLA = atof(ev["ServicelevelPerf"]) / 100.0
		})

	case "QueueMember":
		queue := ev["Queue"]
		h.Queues.Update(tenantID, queue, func(q *monitor.QueueStats) {
			q.Agents++
			if ev["InCall"] == "1" {
				q.InCall++
			}
		})

	case "QueueCallerJoin":
		h.Queues.Update(tenantID, ev["Queue"], func(q *monitor.QueueStats) {
			q.Waiting++
		})

	case "QueueCallerLeave":
		h.Queues.Update(tenantID, ev["Queue"], func(q *monitor.QueueStats) {
			q.Waiting--
		})

	// =====================================================
	// PAUSE
	// =====================================================

	case "QueueMemberPause":
		agent := ev["MemberName"]
		if agent == "" {
			return
		}

		if ev["Paused"] == "1" {
			h.setAgentState(tenantID, agent, "paused", "")
		} else {
			h.setAgentState(tenantID, agent, "idle", "")
		}

	// =====================================================
	// CALL FSM
	// =====================================================

	case "DialBegin", "Newstate":
		if ev["ChannelStateDesc"] != "" && ev["ChannelStateDesc"] != "Ringing" {
			return
		}

		agent := extractAgent(ev["Channel"])
		if agent == "" {
			return
		}

		callID := ev["Linkedid"]
		if callID == "" {
			return
		}

		log.Printf("📞 DialBegin/Newstate: callID=%s, agent=%s, from=%s, to=%s, channel=%s", 
			callID, agent, ev["CallerIDNum"], ev["ConnectedLineNum"], ev["Channel"])

		h.Calls.UpdateCall(tenantID, monitor.Call{
			ID:      callID,
			From:    ev["CallerIDNum"],
			To:      ev["ConnectedLineNum"],
			Channel: ev["Channel"],
		})

		h.setAgentState(tenantID, agent, "ringing", callID)

	case "BridgeEnter":
		agent := extractAgent(ev["Channel"])
		if agent == "" {
			return
		}

		callID := ev["Linkedid"]
		if callID == "" {
			return
		}

		log.Printf("🔗 BridgeEnter: callID=%s, agent=%s, from=%s, to=%s, channel=%s, tenantID=%d", 
			callID, agent, ev["CallerIDNum"], ev["ConnectedLineNum"], ev["Channel"], tenantID)

		call := monitor.Call{
			ID:      callID,
			From:    ev["CallerIDNum"],
			To:      ev["ConnectedLineNum"],
			Channel: ev["Channel"],
		}

		// Сохраняем звонок для текущего tenant
		h.Calls.UpdateCall(tenantID, call)
		log.Printf("💾 Call saved to tenantID=%d, callID=%s, channel=%s", tenantID, callID, call.Channel)

		// 🔥 ДУБЛИРУЕМ звонок для другого участника (если другой tenant)
		otherExt := ev["ConnectedLineNum"]
		if otherExt == agent {
			otherExt = ev["CallerIDNum"]
		}
		
		if otherTenantID := h.Resolver.ResolveByExtension(otherExt); otherTenantID != 0 && otherTenantID != tenantID {
			log.Printf("🔄 Duplicating call to tenantID=%d (other participant)", otherTenantID)
			h.Calls.UpdateCall(otherTenantID, call)
		}

		h.setAgentState(tenantID, agent, "in-call", callID)

	case "Hangup":
		callID := ev["Linkedid"]
		if callID == "" {
			return
		}

		channel := ev["Channel"]
		log.Printf("📴 AMI Hangup: callID=%s, channel=%s, tenantID=%d", callID, channel, tenantID)

		// Проверяем есть ли этот звонок
		calls := h.Calls.GetCalls(tenantID)
		call, exists := calls[callID]
		
		if !exists {
			log.Printf("⚠️ Call not found in CallStore for Hangup: callID=%s, tenantID=%d", callID, tenantID)
			return
		}

		// Удаляем завершившийся канал из массива
		remainingChannels := []string{}
		for _, ch := range call.Channels {
			if ch != channel {
				remainingChannels = append(remainingChannels, ch)
			}
		}
		
		log.Printf("📊 Channels before: %v, after: %v", call.Channels, remainingChannels)

		// Если остались другие каналы - обновляем звонок
		if len(remainingChannels) > 0 {
			call.Channels = remainingChannels
			if len(remainingChannels) > 0 {
				call.Channel = remainingChannels[0] // primary channel
			}
			h.Calls.UpdateCall(tenantID, call)
			log.Printf("✅ Call updated with remaining channels: %v", remainingChannels)
		} else {
			// Все каналы завершены - удаляем звонок и сбрасываем агентов
			log.Printf("🗑️ All channels finished, removing call: callID=%s", callID)
			
			// Сбрасываем всех агентов
			agents := h.Agents.GetAgents(tenantID)
			for _, a := range agents {
				if a.CallID == callID {
					h.Agents.UpdateAgent(tenantID, monitor.AgentState{
						Name:   a.Name,
						Status: "idle",
						CallID: "",
					})
				}
			}
			
			// Удаляем call
			h.Calls.RemoveCall(tenantID, callID)
		}

	// =====================================================
	// PRESENCE FSM
	// =====================================================

	case "PeerStatus":
		agent := extractAgent(ev["Peer"])
		if agent == "" {
			return
		}

		old := h.Agents.GetAgents(tenantID)[agent]
		if old.Status == "ringing" || old.Status == "in-call" {
			return
		}

		if ev["PeerStatus"] == "Reachable" {
			h.setAgentState(tenantID, agent, "idle", "")
		} else {
			h.setAgentState(tenantID, agent, "offline", "")
		}

	case "DeviceStateChange":
		agent := extractAgentFromDevice(ev["Device"])
		if agent == "" {
			return
		}

		old := h.Agents.GetAgents(tenantID)[agent]
		if old.Status == "ringing" || old.Status == "in-call" {
			return
		}

		if ev["State"] == "NOT_INUSE" {
			h.setAgentState(tenantID, agent, "idle", "")
		}
	}
}

// =====================================================
// FSM SETTER
// =====================================================

func (h *Handler) setAgentState(tenantID int, agent, status, callID string) {
	h.Agents.UpdateAgent(tenantID, monitor.AgentState{
		Name:   agent,
		Status: status,
		CallID: callID,
	})
}

// =====================================================
// HELPERS
// =====================================================

func extractAgent(ch string) string {
	if !strings.Contains(ch, "/") {
		return ""
	}
	p := strings.Split(ch, "/")
	if len(p) < 2 {
		return ""
	}
	return strings.Split(p[1], "-")[0]
}

func extractAgentFromDevice(device string) string {
	if strings.HasPrefix(device, "PJSIP/") {
		return strings.TrimPrefix(device, "PJSIP/")
	}
	if strings.Contains(device, "PJSIP/") {
		p := strings.Split(device, "PJSIP/")
		if len(p) == 2 {
			return p[1]
		}
	}
	return ""
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func atof(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}