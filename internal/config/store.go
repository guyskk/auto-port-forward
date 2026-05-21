package config

import (
	"sync"
)

// Store 管理一个 Config 文件的生命周期，所有 mutate 自动落盘。
//
// 并发安全：内部用 RWMutex 保护 cfg；持久化在锁外执行以缩短临界区。
type Store struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

// NewStore 从 path 加载配置，文件缺失时返回 DefaultConfig 但不立刻建文件。
// TOML 损坏时返回 ErrInvalidConfig wrap。
func NewStore(path string) (*Store, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{cfg: cfg, path: path}, nil
}

// Snapshot 返回内部 Config 的深拷贝；调用方可安全修改。
func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

// Rules 返回规则的拷贝。
func (s *Store) Rules() Rules {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRules(s.cfg.Rules)
}

// EnabledHosts 返回启用别名列表的拷贝。
func (s *Store) EnabledHosts() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneStrings(s.cfg.EnabledHosts)
}

// SetHostEnabled 设置某 SSH 别名是否启用监控并持久化。
//
// on=true: 别名不在列表则追加（去重，幂等）。
// on=false: 别名在列表则移除；不在则 no-op，不报错。
//
// 孤儿别名状态：本方法不查询 ssh config，只按字符串集合操作。
// 如果用户在 ssh config 里移除某 host，再添加回来时启用状态会自动恢复。
func (s *Store) SetHostEnabled(alias string, on bool) error {
	s.mu.Lock()
	hosts := append([]string(nil), s.cfg.EnabledHosts...)
	found := -1
	for i, h := range hosts {
		if h == alias {
			found = i
			break
		}
	}
	if on {
		if found < 0 {
			hosts = append(hosts, alias)
		}
	} else {
		if found >= 0 {
			hosts = append(hosts[:found], hosts[found+1:]...)
		}
	}
	s.cfg.EnabledHosts = hosts
	cfg := cloneConfig(s.cfg)
	s.mu.Unlock()
	return Save(s.path, cfg)
}

// UpdateRules 替换 Rules 并持久化。
func (s *Store) UpdateRules(r Rules) error {
	s.mu.Lock()
	s.cfg.Rules = cloneRules(r)
	cfg := cloneConfig(s.cfg)
	s.mu.Unlock()
	return Save(s.path, cfg)
}

// UpdateScanInterval 替换扫描周期秒数并持久化。<=0 时强制改为 15。
func (s *Store) UpdateScanInterval(sec int) error {
	if sec <= 0 {
		sec = 15
	}
	s.mu.Lock()
	s.cfg.ScanIntervalSec = sec
	cfg := cloneConfig(s.cfg)
	s.mu.Unlock()
	return Save(s.path, cfg)
}

func cloneConfig(c Config) Config {
	out := c
	out.Rules = cloneRules(c.Rules)
	out.EnabledHosts = cloneStrings(c.EnabledHosts)
	return out
}

func cloneRules(r Rules) Rules {
	out := r
	if r.ExcludePorts != nil {
		out.ExcludePorts = append([]int(nil), r.ExcludePorts...)
	}
	if r.ExcludeRanges != nil {
		out.ExcludeRanges = append([]Span(nil), r.ExcludeRanges...)
	}
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
