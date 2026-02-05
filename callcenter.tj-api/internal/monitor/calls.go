package monitor

import (
	"sync"
	"time"
)

type CallState string

const (
	CallRinging CallState = "RINGING"
	CallActive  CallState = "ACTIVE"
	CallEnded   CallState = "ENDED"
)

type Call struct {
	ID        string    `json:"id"`        // Linkedid
	From      string    `json:"from"`
	To        string    `json:"to"`
	State     CallState `json:"state"`
	TenantID  int       `json:"tenantId"`
	StartTime int64     `json:"startTime"`
	Duration  int64     `json:"duration"`
}

type CallSubscriber chan Call

type CallStore struct {
	mu    sync.RWMutex
	calls map[int]map[string]Call
	subs  map[int][]CallSubscriber
}

func NewCallStore() *CallStore {
	return &CallStore{
		calls: make(map[int]map[string]Call),
		subs:  make(map[int][]CallSubscriber),
	}
}

// =======================
// UPSERT
// =======================

func (s *CallStore) Upsert(call Call) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.calls[call.TenantID]; !ok {
		s.calls[call.TenantID] = make(map[string]Call)
	}

	s.calls[call.TenantID][call.ID] = call

	for _, sub := range s.subs[call.TenantID] {
		select {
		case sub <- call:
		default:
		}
	}
}

// =======================
// END
// =======================

func (s *CallStore) End(tenantID int, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call, ok := s.calls[tenantID][id]
	if !ok {
		return
	}

	call.State = CallEnded
	call.Duration = int64(time.Since(time.Unix(call.StartTime, 0)).Seconds())

	for _, sub := range s.subs[tenantID] {
		select {
		case sub <- call:
		default:
		}
	}

	delete(s.calls[tenantID], id)
}

// =======================
// SNAPSHOT
// =======================

func (s *CallStore) Snapshot(tenantID int) []Call {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var res []Call
	for _, c := range s.calls[tenantID] {
		res = append(res, c)
	}
	return res
}

// =======================
// SUBSCRIBE
// =======================

func (s *CallStore) Subscribe(tenantID int) CallSubscriber {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(CallSubscriber, 10)
	s.subs[tenantID] = append(s.subs[tenantID], ch)
	return ch
}

func (s *CallStore) Unsubscribe(tenantID int, sub CallSubscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.subs[tenantID]
	for i, ssub := range subs {
		if ssub == sub {
			s.subs[tenantID] = append(subs[:i], subs[i+1:]...)
			close(ssub)
			break
		}
	}
}
