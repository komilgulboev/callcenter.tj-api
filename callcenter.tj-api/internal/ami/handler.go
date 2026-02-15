package ami

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	
	"callcentrix/internal/monitor"
)

type Handler struct {
	Agents         *monitor.Store
	Calls          *monitor.CallStore
	Queues         *monitor.QueueStore
	Resolver       *monitor.TenantResolver
	ipCache        map[string]string
	ipMu           sync.RWMutex
	activeChannels map[string]bool // Трекер активных каналов
	channelsMu     sync.RWMutex
}

func (h *Handler) HandleEvent(ev map[string]string) {

	// 🌐 ContactStatus обрабатываем ДО проверки tenantID
	if ev["Event"] == "ContactStatus" {
		aor := ev["AOR"]
		uri := ev["URI"]
		status := ev["ContactStatus"]
		
		// Обрабатываем только когда контакт Reachable
		if aor != "" && uri != "" && status == "Reachable" {
			ipAddress := extractIPFromURI(uri)
			log.Printf("🌐 ContactStatus: endpoint=%s, ip=%s, status=%s", aor, ipAddress, status)
			
			// Сохраняем в кэш
			h.ipMu.Lock()
			if h.ipCache == nil {
				h.ipCache = make(map[string]string)
			}
			h.ipCache[aor] = ipAddress
			h.ipMu.Unlock()
			log.Printf("💾 Cached IP for %s: %s", aor, ipAddress)
			
			h.updateAgentIP(aor, ipAddress)
		}
		return
	}

	tenantID := h.Resolver.Resolve(ev)
	if tenantID == 0 {
		return
	}

	switch ev["Event"] {

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
		queue := ev["Queue"]
		uniqueID := ev["Uniqueid"]
		callerID := ev["CallerIDNum"]
		
		h.Queues.Update(tenantID, queue, func(q *monitor.QueueStats) {
			q.Waiting++
		})
		
		// Добавляем звонок в список звонков
		h.Calls.UpdateCall(tenantID, monitor.Call{
			ID:        uniqueID,
			From:      callerID,
			To:        queue,
			Channel:   ev["Channel"],
			StartedAt: time.Now(),
		})
		log.Printf("📞 Caller %s joined queue %s (uniqueID: %s)", callerID, queue, uniqueID)

	case "QueueCallerLeave":
		queue := ev["Queue"]
		uniqueID := ev["Uniqueid"]
		
		h.Queues.Update(tenantID, queue, func(q *monitor.QueueStats) {
			q.Waiting--
		})
		
		// Удаляем звонок из списка если он не был отвечен
		// (если отвечен, он уже привязан к агенту через DialBegin/BridgeEnter)
		calls := h.Calls.GetCalls(tenantID)
		if call, exists := calls[uniqueID]; exists {
			// Проверяем не обрабатывается ли звонок агентом
			agents := h.Agents.GetAgents(tenantID)
			isHandled := false
			for _, agent := range agents {
				if agent.CallID == uniqueID {
					isHandled = true
					break
				}
			}
			
			// Удаляем только если не обрабатывается
			if !isHandled {
				h.Calls.RemoveCall(tenantID, uniqueID)
				log.Printf("📴 Caller left queue %s before being answered (uniqueID: %s)", queue, uniqueID)
			} else {
				log.Printf("ℹ️ Caller left queue %s but is being handled by agent (call: %s)", queue, call.From)
			}
		}

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

		h.Calls.UpdateCall(tenantID, call)
		log.Printf("💾 Call saved to tenantID=%d, callID=%s, channel=%s", tenantID, callID, call.Channel)

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

		calls := h.Calls.GetCalls(tenantID)
		call, exists := calls[callID]
		
		if !exists {
			log.Printf("⚠️ Call not found in CallStore for Hangup: callID=%s, tenantID=%d", callID, tenantID)
			return
		}

		remainingChannels := []string{}
		for _, ch := range call.Channels {
			if ch != channel {
				remainingChannels = append(remainingChannels, ch)
			}
		}
		
		log.Printf("📊 Channels before: %v, after: %v", call.Channels, remainingChannels)

		if len(remainingChannels) > 0 {
			call.Channels = remainingChannels
			if len(remainingChannels) > 0 {
				call.Channel = remainingChannels[0]
			}
			h.Calls.UpdateCall(tenantID, call)
			log.Printf("✅ Call updated with remaining channels: %v", remainingChannels)
		} else {
			log.Printf("🗑️ All channels finished, removing call: callID=%s", callID)
			
			// Сбрасываем всех агентов, сохраняя их IP адрес
			agents := h.Agents.GetAgents(tenantID)
			for _, a := range agents {
				if a.CallID == callID {
					log.Printf("🔄 Resetting agent %s to idle (was %s)", a.Name, a.Status)
					h.Agents.UpdateAgent(tenantID, monitor.AgentState{
						Name:      a.Name,
						Status:    "idle",
						CallID:    "",
						IPAddress: a.IPAddress, // Сохраняем IP!
					})
				}
			}
			
			h.Calls.RemoveCall(tenantID, callID)
		}

	case "PeerStatus":
		agent := extractAgent(ev["Peer"])
		if agent == "" {
			return
		}

		old := h.Agents.GetAgents(tenantID)[agent]
		if old.Status == "ringing" || old.Status == "in-call" {
			return
		}

		ipAddress := extractIPFromAddress(ev["Address"])

		if ev["PeerStatus"] == "Reachable" {
			h.setAgentStateWithIP(tenantID, agent, "idle", "", ipAddress)
		} else {
			h.setAgentStateWithIP(tenantID, agent, "offline", "", ipAddress)
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
	
	// Обработка активных каналов для очистки завершённых звонков
	case "CoreShowChannel":
		// Собираем активные каналы
		linkedID := ev["LinkedId"]
		if linkedID != "" {
			h.channelsMu.Lock()
			if h.activeChannels == nil {
				h.activeChannels = make(map[string]bool)
			}
			h.activeChannels[linkedID] = true
			h.channelsMu.Unlock()
		}
	
	case "CoreShowChannelsComplete":
		// Когда получили полный список каналов, очищаем завершённые звонки
		h.channelsMu.Lock()
		activeChannels := make(map[string]bool)
		for k, v := range h.activeChannels {
			activeChannels[k] = v
		}
		h.activeChannels = make(map[string]bool) // Сброс для следующей итерации
		h.channelsMu.Unlock()
		
		// Проходим по всем звонкам всех tenants
		for checkTenantID := 110001; checkTenantID < 999999; checkTenantID++ {
			calls := h.Calls.GetCalls(checkTenantID)
			if len(calls) == 0 {
				continue
			}
			
			for callID := range calls {
				// Если звонка нет в списке активных каналов - удаляем
				if !activeChannels[callID] {
					log.Printf("🧹 Cleaning up stale call: callID=%s, tenant=%d (not in active channels)", callID, checkTenantID)
					
					// Сбрасываем агентов с этим звонком
					agents := h.Agents.GetAgents(checkTenantID)
					for _, a := range agents {
						if a.CallID == callID {
							log.Printf("🔄 Resetting agent %s to idle (stale call cleanup)", a.Name)
							h.Agents.UpdateAgent(checkTenantID, monitor.AgentState{
								Name:      a.Name,
								Status:    "idle",
								CallID:    "",
								IPAddress: a.IPAddress,
							})
						}
					}
					
					// Удаляем звонок
					h.Calls.RemoveCall(checkTenantID, callID)
				}
			}
		}
	}
}

func (h *Handler) setAgentState(tenantID int, agent, status, callID string) {
	// Проверяем есть ли IP в кэше
	h.ipMu.RLock()
	cachedIP := h.ipCache[agent]
	h.ipMu.RUnlock()
	
	if cachedIP != "" {
		log.Printf("📌 Using cached IP for %s: %s", agent, cachedIP)
		h.setAgentStateWithIP(tenantID, agent, status, callID, cachedIP)
	} else {
		h.setAgentStateWithIP(tenantID, agent, status, callID, "")
	}
}

func (h *Handler) setAgentStateWithIP(tenantID int, agent, status, callID, ipAddress string) {
	h.Agents.UpdateAgent(tenantID, monitor.AgentState{
		Name:      agent,
		Status:    status,
		CallID:    callID,
		IPAddress: ipAddress,
	})
}

func (h *Handler) updateAgentIP(agentName, ipAddress string) {
	for tenantID := 110001; tenantID < 999999; tenantID++ {
		agents := h.Agents.GetAgents(tenantID)
		if agent, exists := agents[agentName]; exists {
			h.setAgentStateWithIP(tenantID, agentName, agent.Status, agent.CallID, ipAddress)
			log.Printf("✅ Updated IP for tenant=%d agent=%s: %s", tenantID, agentName, ipAddress)
		}
	}
}

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

func extractIPFromAddress(address string) string {
	if address == "" {
		return ""
	}
	
	parts := strings.Split(address, "/")
	if len(parts) >= 3 {
		ip := parts[2]
		ip = strings.Trim(ip, "[]")
		return ip
	}
	
	return ""
}

func extractIPFromURI(uri string) string {
	if uri == "" {
		return ""
	}
	
	atIndex := strings.Index(uri, "@")
	if atIndex == -1 {
		return ""
	}
	
	afterAt := uri[atIndex+1:]
	
	semiIndex := strings.Index(afterAt, ";")
	if semiIndex != -1 {
		afterAt = afterAt[:semiIndex]
	}
	
	colonIndex := strings.Index(afterAt, ":")
	if colonIndex != -1 {
		afterAt = afterAt[:colonIndex]
	}
	
	return afterAt
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