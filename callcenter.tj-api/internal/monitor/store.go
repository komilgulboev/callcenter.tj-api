package monitor

import "sync"

// =======================
// MODELS
// =======================

type AgentState struct {
	Exten    string `json:"exten"`
	State    string `json:"state"`
	TenantID int    `json:"tenantId"`
}

type Subscriber chan AgentState

// =======================
// STORE (AGENTS ONLY)
// =======================

type Store struct {
	mu sync.RWMutex

	// tenantID → exten → agent
	agents map[int]map[string]AgentState

	// tenantID → subscribers
	subscribers map[int][]Subscriber
}

// =======================
// CONSTRUCTOR
// =======================

func NewStore() *Store {
	return &Store{
		agents:      make(map[int]map[string]AgentState),
		subscribers: make(map[int][]Subscriber),
	}
}

// =======================
// UPDATE AGENT
// =======================

func (s *Store) UpdateAgent(tenantID int, exten, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.agents[tenantID]; !ok {
		s.agents[tenantID] = make(map[string]AgentState)
	}

	agent := AgentState{
		Exten:    exten,
		State:    state,
		TenantID: tenantID,
	}

	s.agents[tenantID][exten] = agent

	for _, sub := range s.subscribers[tenantID] {
		select {
		case sub <- agent:
		default:
		}
	}
}

// =======================
// SNAPSHOT
// =======================

func (s *Store) Snapshot(tenantID int) []AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var res []AgentState
	for _, a := range s.agents[tenantID] {
		res = append(res, a)
	}
	return res
}

// =======================
// WS SUBSCRIBE
// =======================

func (s *Store) Subscribe(tenantID int) Subscriber {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(Subscriber, 10)
	s.subscribers[tenantID] = append(s.subscribers[tenantID], ch)
	return ch
}

func (s *Store) Unsubscribe(tenantID int, sub Subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.subscribers[tenantID]
	for i, ssub := range subs {
		if ssub == sub {
			s.subscribers[tenantID] = append(subs[:i], subs[i+1:]...)
			close(ssub)
			break
		}
	}
}
