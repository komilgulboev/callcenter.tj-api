package ami

import (
	"log"
	"strings"

	"callcentrix/internal/monitor"
)

type Handler struct {
	Agents   *monitor.Store
	Calls    *monitor.CallStore
	Resolver *monitor.TenantResolver
}

func (h *Handler) HandleEvent(event map[string]string) {

	ev := event["Event"]

	tenantID := h.Resolver.Resolve(event)
	if tenantID <= 0 {
		return
	}

	log.Printf(
		"AMI Event=%s Channel=%s → tenant=%d",
		ev,
		event["Channel"],
		tenantID,
	)

	switch ev {

	case "DeviceStateChange":
		h.handleDeviceStateChange(tenantID, event)

	case "DialBegin":
		h.handleDialBegin(tenantID, event)

	case "BridgeEnter":
		h.handleBridgeEnter(tenantID, event)

	case "Hangup":
		h.handleHangup(tenantID, event)
	}
}

// =========================
// DEVICE STATE
// =========================

func (h *Handler) handleDeviceStateChange(tenantID int, e map[string]string) {

	device := e["Device"] // PJSIP/110001
	if !strings.HasPrefix(device, "PJSIP/") {
		return
	}

	agent := strings.TrimPrefix(device, "PJSIP/")
	state := strings.ToUpper(e["State"])

	status := mapDeviceState(state)

	h.Agents.UpdateAgent(tenantID, monitor.AgentState{
		Name:   agent,
		Status: status,
	})
}

func mapDeviceState(state string) string {
	switch state {
	case "NOT_INUSE":
		return "idle"
	case "INUSE", "BUSY", "ONHOLD", "UNKNOWN":
		return "in-call"
	case "RINGING":
		return "ringing"
	case "UNAVAILABLE", "INVALID":
		return "offline"
	default:
		return "idle"
	}
}

// =========================
// DIAL BEGIN
// =========================

func (h *Handler) handleDialBegin(tenantID int, e map[string]string) {

	callID := e["Linkedid"]
	if callID == "" {
		callID = e["Uniqueid"]
	}

	call := monitor.Call{
		ID:    callID,
		From:  e["CallerIDNum"],
		To:    e["DestCallerIDNum"],
		State: "dialing",
	}

	h.Calls.UpdateCall(tenantID, call)

	agent := extractAgentFromChannel(e["Channel"])
	if agent != "" {
		h.Agents.UpdateAgent(tenantID, monitor.AgentState{
			Name:   agent,
			Status: "ringing",
			CallID: callID,
		})
	}
}

// =========================
// BRIDGE ENTER
// =========================

func (h *Handler) handleBridgeEnter(tenantID int, e map[string]string) {

	callID := e["Linkedid"]
	if callID == "" {
		return
	}

	call := monitor.Call{
		ID:    callID,
		From:  e["CallerIDNum"],
		To:    e["ConnectedLineNum"],
		State: "in-call",
	}

	h.Calls.UpdateCall(tenantID, call)

	agent := extractAgentFromChannel(e["Channel"])
	if agent != "" {
		h.Agents.UpdateAgent(tenantID, monitor.AgentState{
			Name:   agent,
			Status: "in-call",
			CallID: callID,
		})
	}
}

// =========================
// HANGUP
// =========================

func (h *Handler) handleHangup(tenantID int, e map[string]string) {

	callID := e["Linkedid"]
	if callID == "" {
		callID = e["Uniqueid"]
	}

	h.Calls.RemoveCall(tenantID, callID)

	agent := extractAgentFromChannel(e["Channel"])
	if agent != "" {
		h.Agents.UpdateAgent(tenantID, monitor.AgentState{
			Name:   agent,
			Status: "idle",
			CallID: "",
		})
	}
}

// =========================
// HELPERS
// =========================

func extractAgentFromChannel(ch string) string {
	if strings.HasPrefix(ch, "PJSIP/") {
		rest := strings.TrimPrefix(ch, "PJSIP/")
		if idx := strings.Index(rest, "-"); idx > 0 {
			return rest[:idx]
		}
		return rest
	}
	return ""
}
