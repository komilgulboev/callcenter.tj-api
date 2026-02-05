package monitor

import "sync"

type QueueStore struct {
	mu     sync.RWMutex
	queues map[int]*QueueStats // tenantID → stats
}

func NewQueueStore() *QueueStore {
	return &QueueStore{
		queues: make(map[int]*QueueStats),
	}
}

func (s *QueueStore) Update(tenantID int, stats QueueStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queues[tenantID] = &stats
}

func (s *QueueStore) Snapshot(tenantID int) *QueueStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queues[tenantID]
}
