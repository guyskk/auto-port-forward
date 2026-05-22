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

// DisabledPorts 返回某 alias 的禁用端口快照（已排序、已去重的拷贝；alias 不存在返回 []int{}）。
func (s *Store) DisabledPorts(alias string) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneInts(s.cfg.DisabledPorts[alias])
}

// SetForwardEnabled 设置 alias:port 是否启用并持久化。
//
// on=true:  从 alias 的禁用列表中移除该端口（不存在则 no-op，幂等）。
// on=false: 加入 alias 的禁用列表（已存在则 no-op，幂等）。
//
// 禁用列表存储时去重并按升序排序，保证 JSON / TOML 输出稳定。
// 当某 alias 的禁用列表清空时，从 map 中删除该 key，避免空数组堆积。
func (s *Store) SetForwardEnabled(alias string, port int, on bool) error {
	s.mu.Lock()
	if s.cfg.DisabledPorts == nil {
		s.cfg.DisabledPorts = map[string][]int{}
	}
	cur := append([]int(nil), s.cfg.DisabledPorts[alias]...)
	next := toggleInSet(cur, port, !on)
	if len(next) == 0 {
		delete(s.cfg.DisabledPorts, alias)
	} else {
		s.cfg.DisabledPorts[alias] = next
	}
	cfg := cloneConfig(s.cfg)
	s.mu.Unlock()
	return Save(s.path, cfg)
}

// toggleInSet 把 port 是否在 set 内调整为 want（true=应在内，false=应不在内）。
// 输入 set 不必有序；返回值去重且升序排序。
func toggleInSet(set []int, port int, want bool) []int {
	has := false
	out := make([]int, 0, len(set)+1)
	for _, p := range set {
		if p == port {
			if has {
				continue // 去重
			}
			has = true
			if want {
				out = append(out, p)
			}
			continue
		}
		out = append(out, p)
	}
	if want && !has {
		out = append(out, port)
	}
	sortInts(out)
	return out
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
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
	out.DisabledPorts = cloneDisabledPorts(c.DisabledPorts)
	return out
}

// cloneDisabledPorts 深拷贝 alias→ports map；nil 输入返回 map[string][]int{}
// （非 nil 空 map），保证 JSON 序列化为 `{}` 而非 `null`，便于前端无脑 `obj[alias]`。
func cloneDisabledPorts(in map[string][]int) map[string][]int {
	out := make(map[string][]int, len(in))
	for k, v := range in {
		out[k] = cloneInts(v)
	}
	return out
}

func cloneRules(r Rules) Rules {
	return Rules{
		ExcludePorts:  cloneInts(r.ExcludePorts),
		ExcludeRanges: cloneSpans(r.ExcludeRanges),
	}
}

// cloneStrings 返回输入的拷贝；输入为 nil 时返回 []string{} 而非 nil，
// 保证经 JSON 序列化后字段是 [] 而非 null，前端可以安全调用 .includes()。
func cloneStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// cloneInts 同 cloneStrings 的语义：nil → [] 而非 nil。
func cloneInts(in []int) []int {
	out := make([]int, len(in))
	copy(out, in)
	return out
}

// cloneSpans 同 cloneStrings 的语义：nil → [] 而非 nil。
func cloneSpans(in []Span) []Span {
	out := make([]Span, len(in))
	copy(out, in)
	return out
}
