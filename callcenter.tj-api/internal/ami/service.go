package ami

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type Service struct {
	addr     string
	username string
	password string
	onEvent  func(map[string]string)

	conn net.Conn
	mu   sync.Mutex
}

func NewService(
	addr string,
	username string,
	password string,
	onEvent func(map[string]string),
) (*Service, error) {
	return &Service{
		addr:     addr,
		username: username,
		password: password,
		onEvent:  onEvent,
	}, nil
}

func (s *Service) Start() {
	conn, err := net.Dial("tcp", s.addr)
	if err != nil {
		log.Println("AMI connect error:", err)
		return
	}
	s.conn = conn

	// =========================
	// LOGIN
	// =========================
	fmt.Fprintf(
		conn,
		"Action: Login\r\nUsername: %s\r\nSecret: %s\r\nEvents: on\r\n\r\n",
		s.username,
		s.password,
	)

	log.Println("✅ AMI connected")

	// =========================
	// INITIAL SNAPSHOTS
	// =========================
	go func() {
		time.Sleep(500 * time.Millisecond)

		log.Println("📡 AMI: requesting DeviceStateList")
		_ = s.SendAction("DeviceStateList", nil)

		log.Println("📡 AMI: requesting QueueStatus") // 🔥 ВОТ ЗДЕСЬ ЗАПРОС ОЧЕРЕДЕЙ
		_ = s.SendAction("QueueStatus", nil)
	}()

	reader := bufio.NewReader(conn)
	event := map[string]string{}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Println("AMI read error:", err)
			return
		}

		// 🔥🔥🔥 СЫРОЙ ЛОГ AMI — САМОЕ ВАЖНОЕ
		//log.Printf("AMI RAW: %s", line)

		line = strings.TrimSpace(line)

		if line == "" {
			if _, ok := event["Event"]; ok {
				s.onEvent(event)
			}
			event = map[string]string{}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			event[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
}

func (s *Service) SendAction(action string, fields map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Println("➡️ AMI SEND:", action, fields)

	if s.conn == nil {
		log.Println("❌ AMI not connected")
		return fmt.Errorf("AMI not connected")
	}

	fmt.Fprintf(s.conn, "Action: %s\r\n", action)
	for k, v := range fields {
		fmt.Fprintf(s.conn, "%s: %s\r\n", k, v)
	}
	fmt.Fprint(s.conn, "\r\n")

	return nil
}