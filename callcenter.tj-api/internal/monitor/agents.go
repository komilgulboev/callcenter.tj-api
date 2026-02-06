package monitor

// AgentState уже ОБЪЯВЛЕН здесь — это правильно
type AgentState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	CallID string `json:"callId,omitempty"`
}

type AgentEvent struct{}

func (s *Store) UpdateAgent(tenantID int, a AgentState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.getTenant(tenantID)

	// 🔥 НЕ ЗАТИРАЕМ CallID
	if prev, ok := t.Agents[a.Name]; ok {
		if a.CallID == "" {
			a.CallID = prev.CallID
		}
	}

	copy := a
	t.Agents[a.Name] = &copy

	for _, ch := range t.agentSubs {
		select {
		case ch <- AgentEvent{}:
		default:
		}
	}
}

func (s *Store) GetAgents(tenantID int) map[string]AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tenants[tenantID]
	if !ok {
		return map[string]AgentState{}
	}

	out := make(map[string]AgentState)
	for k, v := range t.Agents {
		out[k] = *v
	}
	return out
}
