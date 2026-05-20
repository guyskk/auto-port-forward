package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrServerNotFound 在 UpdateServer/DeleteServer 找不到目标 ID 时返回。
var ErrServerNotFound = errors.New("server not found")

// ErrDuplicateServerID 在 AddServer 收到已存在的 ID 时返回。
var ErrDuplicateServerID = errors.New("duplicate server id")

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

// Servers 返回服务器列表的拷贝。
func (s *Store) Servers() []Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneServers(s.cfg.Servers)
}

// Rules 返回规则的拷贝。
func (s *Store) Rules() Rules {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRules(s.cfg.Rules)
}

// GetServer 按 ID 查找；ok=false 表示不存在。
func (s *Store) GetServer(id string) (Server, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sv := range s.cfg.Servers {
		if sv.ID == id {
			return sv, true
		}
	}
	return Server{}, false
}

// AddServer 添加一个 server。ID 为空时自动生成；重复 ID 返回 ErrDuplicateServerID。
// 操作完成后立刻持久化。
func (s *Store) AddServer(in Server) (Server, error) {
	s.mu.Lock()
	if in.ID == "" {
		in.ID = GenerateID()
	} else {
		for _, sv := range s.cfg.Servers {
			if sv.ID == in.ID {
				s.mu.Unlock()
				return Server{}, fmt.Errorf("%w: %s", ErrDuplicateServerID, in.ID)
			}
		}
	}
	if in.Port == 0 {
		in.Port = 22
	}
	s.cfg.Servers = append(s.cfg.Servers, in)
	cfg := cloneConfig(s.cfg)
	s.mu.Unlock()
	if err := Save(s.path, cfg); err != nil {
		return Server{}, err
	}
	return in, nil
}

// UpdateServer 按 ID 替换；找不到返回 ErrServerNotFound。
func (s *Store) UpdateServer(in Server) error {
	s.mu.Lock()
	idx := -1
	for i, sv := range s.cfg.Servers {
		if sv.ID == in.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrServerNotFound, in.ID)
	}
	if in.Port == 0 {
		in.Port = 22
	}
	s.cfg.Servers[idx] = in
	cfg := cloneConfig(s.cfg)
	s.mu.Unlock()
	return Save(s.path, cfg)
}

// DeleteServer 按 ID 删除；找不到返回 ErrServerNotFound。
func (s *Store) DeleteServer(id string) error {
	s.mu.Lock()
	idx := -1
	for i, sv := range s.cfg.Servers {
		if sv.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrServerNotFound, id)
	}
	s.cfg.Servers = append(s.cfg.Servers[:idx], s.cfg.Servers[idx+1:]...)
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

// GenerateID 生成一个时间戳 + 6 字节随机 hex 的服务器 ID，足够避免人工冲突。
func GenerateID() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 极少触发：rand.Reader 在所有支持平台都不会返回错误。
		return fmt.Sprintf("srv-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("srv-%d-%s", time.Now().Unix(), hex.EncodeToString(buf[:]))
}

func cloneConfig(c Config) Config {
	out := c
	out.Servers = cloneServers(c.Servers)
	out.Rules = cloneRules(c.Rules)
	return out
}

func cloneServers(in []Server) []Server {
	if in == nil {
		return nil
	}
	out := make([]Server, len(in))
	copy(out, in)
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
