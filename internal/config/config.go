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
	Lo int `toml:"lo" json:"lo"`
	Hi int `toml:"hi" json:"hi"`
}

// Rules 控制扫描端口的筛选策略。
type Rules struct {
	ExcludePorts  []int  `toml:"exclude_ports" json:"exclude_ports"`
	ExcludeRanges []Span `toml:"exclude_ranges" json:"exclude_ranges"`
}

// Config 是顶层配置结构。
//
// EnabledHosts 是启用监控的 SSH config 别名集合：
// 即使该别名当前已从 ssh config 移除，也保留状态，同名再现时自动恢复。
//
// DisabledPorts 记录每个 alias 被用户明确禁用的远端端口集合（已去重已排序）。
// 与 EnabledHosts 一样，孤儿别名状态保留，同名再现时禁用集合自动恢复。
type Config struct {
	ScanIntervalSec int              `toml:"scan_interval_sec" json:"scan_interval_sec"`
	Rules           Rules            `toml:"rules" json:"rules"`
	EnabledHosts    []string         `toml:"enabled_hosts" json:"enabled_hosts"`
	DisabledPorts   map[string][]int `toml:"disabled_ports" json:"disabled_ports"`
}

// DefaultConfig 返回带默认值的 Config。
func DefaultConfig() Config {
	return Config{
		ScanIntervalSec: 15,
		Rules: Rules{
			ExcludePorts:  []int{22, 53, 80, 443, 111, 631},
			ExcludeRanges: nil,
		},
		EnabledHosts:  nil,
		DisabledPorts: nil,
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

func applyDefaults(c *Config) {
	def := DefaultConfig()
	if c.ScanIntervalSec == 0 {
		c.ScanIntervalSec = def.ScanIntervalSec
	}
	if c.Rules.ExcludePorts == nil {
		c.Rules.ExcludePorts = def.Rules.ExcludePorts
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

// DefaultPath 返回 $UserConfigDir/auto-port-forward/config.toml 的路径。
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auto-port-forward", "config.toml"), nil
}
