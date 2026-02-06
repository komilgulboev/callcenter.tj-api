package monitor

import (
	"sync"
	"time"
)

type Call struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	TenantID  int       `json:"tenantId"`
	StartedAt time.Time `json:"startedAt"`
	State     string    `json:"state"`
}

type CallEvent struct{}

type CallStore struct {
	mu    sync.RWMutex
	items map[int]map[string]Call // tenant → callID → Call
	subs  map[int][]chan CallEvent
}

func NewCallStore() *CallStore {
	return &CallStore{
		items: make(map[int]map[string]Call),
		subs:  make(map[int][]chan CallEvent),
	}
}

func (s *CallStore) GetCalls(tenantID int) map[string]Call {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := map[string]Call{}
	for k, v := range s.items[tenantID] {
		out[k] = v
	}
	return out
}

func (s *CallStore) UpdateCall(tenantID int, call Call) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.items[tenantID] == nil {
		s.items[tenantID] = make(map[string]Call)
	}

	// ⏱️ если первый раз — фиксируем старт
	if old, ok := s.items[tenantID][call.ID]; ok {
		call.StartedAt = old.StartedAt
	} else {
		call.StartedAt = time.Now()
	}

	s.items[tenantID][call.ID] = call

	for _, ch := range s.subs[tenantID] {
		select {
		case ch <- CallEvent{}:
		default:
		}
	}
}

func (s *CallStore) RemoveCall(tenantID int, callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.items[tenantID], callID)

	for _, ch := range s.subs[tenantID] {
		select {
		case ch <- CallEvent{}:
		default:
		}
	}
}

func (s *CallStore) Subscribe(tenantID int, ch chan CallEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs[tenantID] = append(s.subs[tenantID], ch)
}

func (s *CallStore) Unsubscribe(tenantID int, ch chan CallEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.subs[tenantID]
	for i, c := range list {
		if c == ch {
			s.subs[tenantID] = append(list[:i], list[i+1:]...)
			break
		}
	}
}
