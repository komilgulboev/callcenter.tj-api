package monitor

type QueueStats struct {
	Queue       string `json:"queue"`
	Waiting     int    `json:"waiting"`
	AgentsTotal int    `json:"agentsTotal"`
	Missed      int    `json:"missed"`
}
