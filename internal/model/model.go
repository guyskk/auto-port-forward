// Package model 定义跨层共享的数据结构。
// 所有字段保持 JSON/TOML 友好，便于与前端 Wails 绑定及配置文件序列化。
package model

// PortStatus 表示一个远端端口在本程序中的可视状态。
type PortStatus string

const (
	StatusForwarding   PortStatus = "forwarding"     // 绿: 转发已建立
	StatusPending      PortStatus = "pending"        // 灰: 等待转发就绪
	StatusExcluded     PortStatus = "excluded"       // 灰: 命中黑名单/特权
	StatusConflict     PortStatus = "conflict"       // 红: 本地被占用
	StatusConflictPriv PortStatus = "conflict_priv"  // 红: <1024 非 root
	StatusError        PortStatus = "error"          // 红: SSH/forward 错误
)

// RemotePort 描述远端探测到的一个监听端口。
type RemotePort struct {
	Port        int    `json:"port"`
	BindAddr    string `json:"bind_addr"`
	IPVersion   string `json:"ip_version"` // "IPv4" | "IPv6" | "dual"
	PID         int    `json:"pid"`
	Process     string `json:"process"`
	Command     string `json:"command"`
	DockerImage string `json:"docker_image"`
}

// LocalPort 描述本机正在监听的端口（由 sonar 提供）。
type LocalPort struct {
	Port    int    `json:"port"`
	Process string `json:"process"`
	Type    string `json:"type"` // "user" | "system" | etc.
	PID     int    `json:"pid"`
}

// Forward 描述一条远端 → 本地的转发任务及其运行态。
type Forward struct {
	ServerID   string     `json:"server_id"`
	RemotePort int        `json:"remote_port"`
	LocalPort  int        `json:"local_port"`
	Status     PortStatus `json:"status"`
	Error      string     `json:"error,omitempty"`
	LastActive int64      `json:"last_active"`
	Remote     RemotePort `json:"remote"`
}
