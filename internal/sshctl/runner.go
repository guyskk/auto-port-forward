// Package sshctl 通过系统 ssh 命令 + ControlMaster 机制管理与单个 host 的连接。
//
// 设计原则：
//   - 一台 host 一个 Client；一个 Client 持有一个 master 进程的句柄、socket 路径与命令运行能力。
//   - 不参与认证 / known_hosts / ProxyJump 等高级决策，全部委托 OpenSSH 本身。
//   - 所有外部命令调用通过 Runner 接口，便于单测注入 fake，不真 fork ssh。
package sshctl

import (
	"context"
	"os/exec"
)

// Runner 抽象 sshctl 所需的两类命令执行能力。
//
//   - Run: 一次性运行命令，捕获 stdout+stderr，进程退出后返回（如 -O check / -O forward
//     / 远端 exec）。
//   - Start: 启动一个长生命周期进程（master ControlMaster），返回 Process 句柄，
//     供调用方等待退出或主动 kill。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	Start(ctx context.Context, name string, args ...string) (Process, error)
}

// Process 是 Runner.Start 返回的进程句柄。
type Process interface {
	// Wait 在进程退出后通过 channel 投递最终 error（nil 表示正常退出）。
	// Channel 只触发一次。
	Wait() <-chan error
	// Kill 强制结束进程；幂等。
	Kill() error
}

// NewDefaultRunner 返回生产 Runner（exec.CommandContext 包装）。
func NewDefaultRunner() Runner { return defaultRunner{} }

type defaultRunner struct{}

func (defaultRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (defaultRunner) Start(ctx context.Context, name string, args ...string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &defaultProcess{cmd: cmd, waitCh: make(chan error, 1)}
	go func() {
		p.waitCh <- cmd.Wait()
	}()
	return p, nil
}

type defaultProcess struct {
	cmd    *exec.Cmd
	waitCh chan error
}

func (p *defaultProcess) Wait() <-chan error { return p.waitCh }

func (p *defaultProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}
