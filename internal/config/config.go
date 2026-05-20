// Package config 读写 TOML 配置，提供默认值与原子写。
package config

import "errors"

// Span 表示一个端口范围（闭区间）。
type Span struct {
	Lo int `toml:"lo"`
	Hi int `toml:"hi"`
}

// Server 表示一个 SSH 目标服务器配置。
type Server struct {
	ID         string `toml:"id"`
	Name       string `toml:"name"`
	Host       string `toml:"host"`
	Port       int    `toml:"port"` // 默认 22
	User       string `toml:"user"`
	AuthMethod string `toml:"auth_method"` // password | ssh_key | ssh_agent
	Password   string `toml:"password,omitempty"`
	KeyPath    string `toml:"key_path,omitempty"`
	Passphrase string `toml:"passphrase,omitempty"`
	HostKey    string `toml:"host_key"` // known_hosts | insecure
	Enabled    bool   `toml:"enabled"`
}

// Rules 控制转发筛选和本地映射策略。
type Rules struct {
	ExcludePorts    []int  `toml:"exclude_ports"`
	ExcludeRanges   []Span `toml:"exclude_ranges"`
	OnlyPublicBind  bool   `toml:"only_public_bind"`
	LocalPortOffset int    `toml:"local_port_offset"`
}

// Config 是顶层配置结构。
type Config struct {
	ScanIntervalSec int      `toml:"scan_interval_sec"`
	Servers         []Server `toml:"servers"`
	Rules           Rules    `toml:"rules"`
}

// DefaultConfig 返回带默认值的 Config。
func DefaultConfig() Config {
	return Config{
		ScanIntervalSec: 15,
		Servers:         nil,
		Rules: Rules{
			ExcludePorts:    []int{22, 53, 80, 443, 111, 631},
			ExcludeRanges:   nil,
			OnlyPublicBind:  false,
			LocalPortOffset: 0,
		},
	}
}

// ErrInvalidConfig 表示配置文件解析失败或字段非法。
var ErrInvalidConfig = errors.New("invalid config")

// Load 从给定路径读取 TOML 配置文件；不存在时返回 DefaultConfig。
// TODO(M1): 实现 — 缺字段补默认；BOM/空文件处理；坏 TOML 返回 ErrInvalidConfig wrap。
func Load(path string) (Config, error) {
	_ = path
	return DefaultConfig(), nil
}

// Save 原子写 TOML 到给定路径。
// TODO(M1): 实现 — 临时文件 + os.Rename；目录不存在时创建；权限 0600。
func Save(path string, c Config) error {
	_ = path
	_ = c
	return nil
}

// DefaultPath 返回 $UserConfigDir/autoportforward/config.toml 的路径。
// TODO(M1): 用 os.UserConfigDir() 拼出最终路径。
func DefaultPath() (string, error) {
	return "", nil
}
