package ami

import (
	"log"
	"strconv"
	"time"

	"callcentrix/internal/monitor"
)

type Handler struct {
	Agents *monitor.Store
	Calls  *monitor.CallStore
}

func (h *Handler) HandleEvent(event map[string]string) {
	eventName := event["Event"]

	log.Println("AMI RAW EVENT:", event)

	switch eventName {

	// =======================
	// AGENT STATE
	// =======================
	case "DeviceStateChange":
		device := event["Device"]
		state := event["State"]

		exten := parseExten(device)
		if exten == "" {
			return
		}

		tenantID, _ := strconv.Atoi(exten) // ⚠ временно
		h.Agents.UpdateAgent(tenantID, exten, state)

	// =======================
	// NEW CALL
	// =======================
	case "Newchannel":
		linkedid := event["Linkedid"]
		from := event["CallerIDNum"]
		to := event["Exten"]

		if linkedid == "" || from == "" || to == "" {
			return
		}

		tenantID, _ := strconv.Atoi(to)

		h.Calls.Upsert(monitor.Call{
			ID:        linkedid,
			From:      from,
			To:        to,
			State:     monitor.CallRinging,
			TenantID:  tenantID,
			StartTime: time.Now().Unix(),
		})

	// =======================
	// CALL ANSWER
	// =======================
	case "BridgeEnter":
		linkedid := event["Linkedid"]
		exten := event["Exten"]

		if linkedid == "" || exten == "" {
			return
		}

		tenantID, _ := strconv.Atoi(exten)

		h.Calls.Upsert(monitor.Call{
			ID:        linkedid,
			State:     monitor.CallActive,
			TenantID:  tenantID,
			StartTime: time.Now().Unix(),
		})

	// =======================
	// CALL END
	// =======================
	case "Hangup":
		linkedid := event["Linkedid"]
		exten := event["Exten"]

		if linkedid == "" || exten == "" {
			return
		}

		tenantID, _ := strconv.Atoi(exten)
		h.Calls.End(tenantID, linkedid)
	}
}

func parseExten(device string) string {
	for i := len(device) - 1; i >= 0; i-- {
		if device[i] == '/' {
			return device[i+1:]
		}
	}
	return ""
}
