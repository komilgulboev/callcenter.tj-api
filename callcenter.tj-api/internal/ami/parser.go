package ami

import "log"

func HandleEvent(ev map[string]string) {
	evType := ev["Event"]
	if evType == "" {
		return
	}

	tenant := extractTenant(ev)

	switch evType {
	case "QueueMemberStatus":
		log.Printf("[TENANT %s] QueueMember %s status=%s",
			tenant,
			ev["MemberName"],
			ev["Status"],
		)

	case "DialBegin":
		log.Printf("[TENANT %s] Dial %s -> %s",
			tenant,
			ev["CallerIDNum"],
			ev["DialString"],
		)

	case "Hangup":
		log.Printf("[TENANT %s] Hangup %s cause=%s",
			tenant,
			ev["CallerIDNum"],
			ev["Cause"],
		)
	}
}

func extractTenant(ev map[string]string) string {
	for k, v := range ev {
		if k == "Variable" && len(v) > 10 && v[:10] == "TENANT_ID" {
			return v
		}
	}
	return "unknown"
}
