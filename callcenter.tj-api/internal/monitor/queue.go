package monitor

import "sync"

type QueueStats struct {
	Name string `json:"name"`

	Waiting int `json:"waiting"`
	InCall  int `json:"inCall"`
	Agents  int `json:"agents"`

	Completed int     `json:"completed"`
	HoldTime  int     `json:"holdTime"`
	TalkTime  int     `json:"talkTime"`
	SLA       float64 `json:"sla"`
}

type QueueStore struct {
	mu     sync.RWMutex
	queues map[int]map[string]*QueueStats
	subs   map[int][]chan struct{} // 🔔 subscribers per tenant
}

func NewQueueStore() *QueueStore {
	return &QueueStore{
		queues: make(map[int]map[string]*QueueStats),
		subs:   make(map[int][]chan struct{}),
	}
}

func (s *QueueStore) ensure(tenantID int, queue string) *QueueStats {
	if s.queues[tenantID] == nil {
		s.queues[tenantID] = make(map[string]*QueueStats)
	}
	if s.queues[tenantID][queue] == nil {
		s.queues[tenantID][queue] = &QueueStats{Name: queue}
	}
	return s.queues[tenantID][queue]
}

func (s *QueueStore) Update(
	tenantID int,
	queue string,
	fn func(q *QueueStats),
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q := s.ensure(tenantID, queue)
	fn(q)

	// защита
	if q.Waiting < 0 {
		q.Waiting = 0
	}
	if q.InCall < 0 {
		q.InCall = 0
	}
	if q.Agents < 0 {
		q.Agents = 0
	}

	// 🔔 уведомляем подписчиков
	for _, ch := range s.subs[tenantID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *QueueStore) Snapshot(tenantID int) map[string]QueueStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]QueueStats)
	for k, v := range s.queues[tenantID] {
		out[k] = *v
	}
	return out
}

// =========================
// SUBSCRIPTIONS
// =========================
func (s *QueueStore) Subscribe(tenantID int, ch chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.subs[tenantID] = append(s.subs[tenantID], ch)
}

func (s *QueueStore) Unsubscribe(tenantID int, ch chan struct{}) {
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
