package monitor

import "sync"

// =========================
// TENANT STATE
// =========================

type tenantState struct {
	Agents map[string]*AgentState
	Calls  map[string]*Call

	agentSubs []chan AgentEvent
	callSubs  []chan CallEvent
}

// =========================
// STORE
// =========================

type Store struct {
	mu      sync.RWMutex
	tenants map[int]*tenantState
}

func NewStore() *Store {
	return &Store{
		tenants: make(map[int]*tenantState),
	}
}

// =========================
// INTERNAL
// =========================

func (s *Store) getTenant(id int) *tenantState {
	t, ok := s.tenants[id]
	if !ok {
		t = &tenantState{
			Agents: make(map[string]*AgentState),
			Calls:  make(map[string]*Call),
		}
		s.tenants[id] = t
	}
	return t
}

// =========================
// AGENT HELPERS
// =========================

// GetAgent нужен, чтобы НЕ затирать CallID
func (s *Store) GetAgent(tenantID int, name string) *AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if t, ok := s.tenants[tenantID]; ok {
		if a, ok := t.Agents[name]; ok {
			copy := *a
			return &copy
		}
	}
	return nil
}

// =========================
// SUBSCRIBE / UNSUBSCRIBE
// =========================

// --- Agents ---

func (s *Store) SubscribeAgents(tenantID int, ch chan AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.getTenant(tenantID)
	t.agentSubs = append(t.agentSubs, ch)
}

func (s *Store) UnsubscribeAgents(tenantID int, ch chan AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.getTenant(tenantID)
	for i, c := range t.agentSubs {
		if c == ch {
			t.agentSubs = append(t.agentSubs[:i], t.agentSubs[i+1:]...)
			break
		}
	}
}

// --- Calls ---

func (s *Store) SubscribeCalls(tenantID int, ch chan CallEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.getTenant(tenantID)
	t.callSubs = append(t.callSubs, ch)
}

func (s *Store) UnsubscribeCalls(tenantID int, ch chan CallEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.getTenant(tenantID)
	for i, c := range t.callSubs {
		if c == ch {
			t.callSubs = append(t.callSubs[:i], t.callSubs[i+1:]...)
			break
		}
	}
}
