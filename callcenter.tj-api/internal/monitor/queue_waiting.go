package monitor

import (
	"sync"
	"time"
)

// расширение QueueStats — НЕ ЛОМАЕТ существующий JSON
type QueueRuntime struct {
	WaitingSince   map[string]time.Time // uniqueid → enter time
	AnsweredInSLA  int
	AnsweredTotal  int
}

type QueueRuntimeStore struct {
	mu   sync.Mutex
	data map[int]map[string]*QueueRuntime // tenant → queue
}

func NewQueueRuntimeStore() *QueueRuntimeStore {
	return &QueueRuntimeStore{
		data: make(map[int]map[string]*QueueRuntime),
	}
}

func (s *QueueRuntimeStore) ensure(tenantID int, queue string) *QueueRuntime {
	if s.data[tenantID] == nil {
		s.data[tenantID] = make(map[string]*QueueRuntime)
	}
	if s.data[tenantID][queue] == nil {
		s.data[tenantID][queue] = &QueueRuntime{
			WaitingSince: make(map[string]time.Time),
		}
	}
	return s.data[tenantID][queue]
}

// Caller enters queue
func (s *QueueRuntimeStore) OnJoin(
	tenantID int,
	queue string,
	uniqueID string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q := s.ensure(tenantID, queue)
	q.WaitingSince[uniqueID] = time.Now()
}

// Caller leaves / abandons before answer
func (s *QueueRuntimeStore) OnLeave(
	tenantID int,
	queue string,
	uniqueID string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q := s.ensure(tenantID, queue)
	delete(q.WaitingSince, uniqueID)
}

// Caller connected to agent
func (s *QueueRuntimeStore) OnConnect(
	tenantID int,
	queue string,
	uniqueID string,
	sla time.Duration,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q := s.ensure(tenantID, queue)

	enter, ok := q.WaitingSince[uniqueID]
	if !ok {
		return
	}

	wait := time.Since(enter)
	q.AnsweredTotal++

	if wait <= sla {
		q.AnsweredInSLA++
	}

	delete(q.WaitingSince, uniqueID)
}

// SLA snapshot (percent)
func (s *QueueRuntimeStore) SLAPercent(
	tenantID int,
	queue string,
) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	q := s.ensure(tenantID, queue)
	if q.AnsweredTotal == 0 {
		return 100
	}
	return int(
		(float64(q.AnsweredInSLA) / float64(q.AnsweredTotal)) * 100,
	)
}
