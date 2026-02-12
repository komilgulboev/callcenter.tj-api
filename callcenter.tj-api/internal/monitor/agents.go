package monitor

import "sync"

// =========================
// TYPES
// =========================

type AgentState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	CallID string `json:"callId,omitempty"`
	FirstName string `json:"firstName,omitempty"`  // ← ДОБАВИТЬ
    LastName  string `json:"lastName,omitempty"` 
}

type AgentEvent struct {
	TenantID int
	Agent    AgentState
}

// =========================
// STORE
// =========================

type Store struct {
	mu      sync.RWMutex
	tenants map[int]map[string]AgentState
	subs    map[int][]chan AgentEvent
}

func NewStore() *Store {
	return &Store{
		tenants: make(map[int]map[string]AgentState),
		subs:    make(map[int][]chan AgentEvent),
	}
}

// =========================
// PUBLIC API
// =========================

func (s *Store) UpdateAgent(tenantID int, agent AgentState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tenants[tenantID]; !ok {
		s.tenants[tenantID] = make(map[string]AgentState)
	}

	old, ok := s.tenants[tenantID][agent.Name]

	// Проверяем можно ли перезаписать статус
	if ok && !canOverride(old.Status, agent.Status) {
		return
	}

	s.tenants[tenantID][agent.Name] = agent

	// Отправляем событие подписчикам (WebSocket)
	for _, ch := range s.subs[tenantID] {
		select {
		case ch <- AgentEvent{TenantID: tenantID, Agent: agent}:
		default:
		}
	}
}

func (s *Store) GetAgents(tenantID int) map[string]AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]AgentState)
	for k, v := range s.tenants[tenantID] {
		out[k] = v
	}
	return out
}

// =========================
// SUBSCRIPTIONS
// =========================

func (s *Store) SubscribeAgents(tenantID int, ch chan AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs[tenantID] = append(s.subs[tenantID], ch)
}

func (s *Store) UnsubscribeAgents(tenantID int, ch chan AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.subs[tenantID]
	for i, c := range subs {
		if c == ch {
			s.subs[tenantID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func canOverride(old, next string) bool {
	prio := map[string]int{
		"offline": 5,
		"paused":  4,
		"in-call": 3,
		"ringing": 2,
		"idle":    1,
	}

	return prio[next] >= prio[old]
}