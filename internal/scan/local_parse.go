package scan

import (
	"encoding/json"

	"autoportforward/internal/model"
)

// ParseSonarJSON 把 `sonar list --json` 的输出解码成 LocalPort 列表。
// 仅取 port / pid / process / type 字段，其余忽略。
func ParseSonarJSON(out []byte) ([]model.LocalPort, error) {
	if len(out) == 0 {
		return nil, nil
	}
	var raw []struct {
		Port    int    `json:"port"`
		PID     int    `json:"pid"`
		Process string `json:"process"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	ports := make([]model.LocalPort, 0, len(raw))
	for _, r := range raw {
		ports = append(ports, model.LocalPort{
			Port:    r.Port,
			PID:     r.PID,
			Process: r.Process,
			Type:    r.Type,
		})
	}
	return ports, nil
}
