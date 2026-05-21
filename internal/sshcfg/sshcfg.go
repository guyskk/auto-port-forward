// Package sshcfg 提供 SSH 配置文件（~/.ssh/config）的列举与解析能力。
//
// 设计原则：
//   - 列举别名通过 shell 命令（grep/awk）实现，不在 Go 里 parse ssh config 语法。
//   - 解析单个别名的 effective 配置依靠 `ssh -G <alias>`，由 OpenSSH 自己完成。
//   - 所有外部命令调用通过 Runner 接口，便于单测注入 fake。
package sshcfg

import "context"

// Host 表示从 ssh config 解析出的一台目标主机的核心连接参数。
//
// 仅保留连接 ControlMaster 必需的字段。认证 / ProxyJump / known_hosts
// 等高级配置完全交给系统 ssh 处理，APP 不存也不参与决策。
type Host struct {
	Alias    string `json:"alias"`     // ssh config 中具体别名（已过滤通配符）
	HostName string `json:"host_name"` // ssh -G 的 effective hostname
	User     string `json:"user"`      // ssh -G 的 effective user
	Port     int    `json:"port"`      // ssh -G 的 effective port，默认 22
}

// Runner 抽象"执行命令"的能力。
//
// 生产代码使用 DefaultRunner（exec.CommandContext），单测注入 fake 验证命令参数 +
// 输出解析，不真 fork ssh / grep / awk。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// TODO(T2): 实现 ListAliases / Resolve / ListHosts / DefaultRunner。
