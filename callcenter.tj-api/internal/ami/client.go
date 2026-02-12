package ami

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

func NewClient(addr, user, pass string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}

	r := bufio.NewReader(conn)

	// Login
	login := fmt.Sprintf(
		"Action: Login\r\nUsername: %s\r\nSecret: %s\r\nEvents: on\r\n\r\n",
		user,
		pass,
	)

	if _, err := conn.Write([]byte(login)); err != nil {
		return nil, err
	}

	//log.Println("✅ AMI connected")

	return &Client{
		conn: conn,
		r:    r,
	}, nil
}

func (c *Client) ReadEvent() (map[string]string, error) {
	ev := make(map[string]string)

	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			ev[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	return ev, nil
}

func (c *Client) ReadLoop(handler func(map[string]string)) {
	for {
		ev, err := c.ReadEvent()
		if err != nil {
			//log.Println("AMI read error:", err)
			time.Sleep(time.Second)
			continue
		}
		handler(ev)
	}
}