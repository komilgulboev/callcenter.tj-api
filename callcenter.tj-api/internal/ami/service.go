package ami

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
)

type Service struct {
	addr     string
	username string
	password string
	onEvent  func(map[string]string)
}

func NewService(addr, user, pass string, onEvent func(map[string]string)) (*Service, error) {
	return &Service{
		addr:     addr,
		username: user,
		password: pass,
		onEvent:  onEvent,
	}, nil
}

func (s *Service) Start() {
	conn, err := net.Dial("tcp", s.addr)
	if err != nil {
		log.Println("AMI connect error:", err)
		return
	}

	log.Println("✅ AMI connected")

	// Login
	fmt.Fprintf(conn,
		"Action: Login\r\nUsername: %s\r\nSecret: %s\r\nEvents: on\r\n\r\n",
		s.username,
		s.password,
	)

	reader := bufio.NewReader(conn)
	event := map[string]string{}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Println("AMI read error:", err)
			return
		}

		line = strings.TrimSpace(line)

		// empty line = event end
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
