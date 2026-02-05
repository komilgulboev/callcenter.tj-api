package monitor

type Agent struct {
	Exten   string `json:"exten"`
	State   string `json:"state"`
	InCall  bool   `json:"inCall"`
	With    string `json:"with,omitempty"`
}
