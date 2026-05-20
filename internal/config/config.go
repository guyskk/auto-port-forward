// Package config 读写 TOML 配置，提供默认值与原子写。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

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
// 缺失字段在解析后用 DefaultConfig 补齐，TOML 语法错误返回 ErrInvalidConfig wrap。
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, err
	}
	var c Config
	if _, err := toml.Decode(string(data), &c); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	applyDefaults(&c)
	return c, nil
}

// applyDefaults 用 DefaultConfig 的值填充零值字段，仅在 Load 后调用。
func applyDefaults(c *Config) {
	def := DefaultConfig()
	if c.ScanIntervalSec == 0 {
		c.ScanIntervalSec = def.ScanIntervalSec
	}
	if c.Rules.ExcludePorts == nil {
		c.Rules.ExcludePorts = def.Rules.ExcludePorts
	}
	for i := range c.Servers {
		if c.Servers[i].Port == 0 {
			c.Servers[i].Port = 22
		}
	}
}

// Save 原子写 TOML 到给定路径：写临时文件，fsync，rename。
// 目录不存在时自动创建；权限 0600。
func Save(path string, c Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config.toml.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(c); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// DefaultPath 返回 $UserConfigDir/autoportforward/config.toml 的路径。
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autoportforward", "config.toml"), nil
}
