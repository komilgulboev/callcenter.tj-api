package monitor

import (
	"sync"
	"time"
)

// =========================
// CALL MODEL
// =========================

type Call struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Channel   string    `json:"channel"`   // Primary channel (для обратной совместимости)
	Channels  []string  `json:"channels"`  // Все каналы участников звонка
	StartedAt time.Time `json:"startedAt"`
}

// =========================
// CALL STORE
// =========================

type CallStore struct {
	mu          sync.RWMutex
	calls       map[int]map[string]Call // tenantID → callID → Call
	subscribers map[int][]chan struct{} // tenantID → channels
	subMu       sync.RWMutex
}

func NewCallStore() *CallStore {
	return &CallStore{
		calls:       make(map[int]map[string]Call),
		subscribers: make(map[int][]chan struct{}),
	}
}

// =========================
// PUBLIC API
// =========================

func (s *CallStore) UpdateCall(tenantID int, call Call) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.calls[tenantID] == nil {
		s.calls[tenantID] = make(map[string]Call)
	}

	// если звонок новый — фиксируем старт и создаём массив каналов
	if existing, ok := s.calls[tenantID][call.ID]; !ok {
		call.StartedAt = time.Now()
		// Инициализируем массив каналов
		if call.Channel != "" {
			call.Channels = []string{call.Channel}
		}
	} else {
		// Звонок уже существует - сохраняем StartedAt
		call.StartedAt = existing.StartedAt
		
		// Объединяем каналы (добавляем новый если его нет)
		call.Channels = existing.Channels
		if call.Channel != "" {
			channelExists := false
			for _, ch := range call.Channels {
				if ch == call.Channel {
					channelExists = true
					break
				}
			}
			if !channelExists {
				call.Channels = append(call.Channels, call.Channel)
			}
		}
	}

	s.calls[tenantID][call.ID] = call
	
	// ✅ УВЕДОМЛЯЕМ подписчиков!
	s.notifySubscribers(tenantID)
}

func (s *CallStore) RemoveCall(tenantID int, callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.calls[tenantID]; !ok {
		return
	}

	delete(s.calls[tenantID], callID)
	
	// ✅ УВЕДОМЛЯЕМ подписчиков!
	s.notifySubscribers(tenantID)
}

func (s *CallStore) GetCalls(tenantID int) map[string]Call {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]Call)
	for k, v := range s.calls[tenantID] {
		out[k] = v
	}
	return out
}

// =========================
// SUBSCRIPTIONS
// =========================

func (s *CallStore) Subscribe(tenantID int, ch chan struct{}) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	s.subscribers[tenantID] = append(s.subscribers[tenantID], ch)
}

func (s *CallStore) Unsubscribe(tenantID int, ch chan struct{}) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	
	subs := s.subscribers[tenantID]
	for i, sub := range subs {
		if sub == ch {
			s.subscribers[tenantID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func (s *CallStore) notifySubscribers(tenantID int) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	
	for _, ch := range s.subscribers[tenantID] {
		select {
		case ch <- struct{}{}:
		default:
			// Канал заполнен, пропускаем
		}
	}
}