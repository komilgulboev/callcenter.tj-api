package monitor

import "sync"

type QueueStats struct {
	Name   string `json:"name"`
	Calls  int    `json:"calls"`
	Missed int    `json:"missed"`
}

type QueueStore struct {
	mu     sync.RWMutex
	queues map[int]*QueueStats
}

func NewQueueStore() *QueueStore {
	return &QueueStore{
		queues: make(map[int]*QueueStats),
	}
}

func (s *QueueStore) Ensure(tenantID int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.queues[tenantID]; !ok {
		s.queues[tenantID] = &QueueStats{
			Name:   "Queue",
			Calls:  0,
			Missed: 0,
		}
	}
}

func (s *QueueStore) Snapshot(tenantID int) *QueueStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if q, ok := s.queues[tenantID]; ok {
		copy := *q
		return &copy
	}
	return nil
}
