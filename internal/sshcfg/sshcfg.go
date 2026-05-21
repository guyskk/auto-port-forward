// Package sshcfg 提供 SSH 配置文件（~/.ssh/config）的列举与解析能力。
//
// 设计原则：
//   - 列举别名通过 shell 命令（grep/awk）实现，不在 Go 里 parse ssh config 语法。
//   - 解析单个别名的 effective 配置依靠 `ssh -G <alias>`，由 OpenSSH 自己完成。
//   - 所有外部命令调用通过 Runner 接口，便于单测注入 fake。
//
// MVP 不展开 Include 指令、不解析 Match 块；通配符别名（含 * ? !）由 shell pipeline
// 过滤掉。后续按用户反馈追加。
package sshcfg

import (
	"context"
	"os/exec"
)

// Host 表示从 ssh config 解析出的一台目标主机的核心连接参数。
//
// 仅保留连接 ControlMaster 必需的字段。认证 / ProxyJump / known_hosts
// 等高级配置完全交给系统 ssh 处理，APP 不存也不参与决策。
type Host struct {
	Alias    string `json:"alias"`
	HostName string `json:"host_name"`
	User     string `json:"user"`
	Port     int    `json:"port"`
}

// Runner 抽象"执行命令"的能力。
//
// 生产代码使用 DefaultRunner（exec.CommandContext），单测注入 fake 验证命令参数 +
// 输出解析，不真 fork ssh / grep / awk。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewDefaultRunner 返回生产 Runner：调用系统命令，捕获 stdout。
//
// stderr 默认丢弃 — 调用方关心的是 stdout 内容；命令失败由 exit code 表达。
func NewDefaultRunner() Runner {
	return defaultRunner{}
}

type defaultRunner struct{}

func (defaultRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}
