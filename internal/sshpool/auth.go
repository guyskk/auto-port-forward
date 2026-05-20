package sshpool

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"autoportforward/internal/config"
)

// ErrMissingAuth 表示没能构造出任何可用的 SSH 认证方法。
var ErrMissingAuth = errors.New("no usable ssh auth method")

// buildAuthMethods 根据 Server 配置构造 ssh.AuthMethod 列表。
// 纯函数（不打开真实 agent socket 之外的 I/O）。
//
//   - "password"   → 用 cfg.Password
//   - "ssh_key"    → 读 cfg.KeyPath，passphrase 可选
//   - "ssh_agent"  → 通过 $SSH_AUTH_SOCK 拿签名能力
func buildAuthMethods(cfg config.Server) ([]ssh.AuthMethod, error) {
	switch cfg.AuthMethod {
	case "password":
		if cfg.Password == "" {
			return nil, fmt.Errorf("%w: password empty", ErrMissingAuth)
		}
		return []ssh.AuthMethod{ssh.Password(cfg.Password)}, nil
	case "ssh_key":
		if cfg.KeyPath == "" {
			return nil, fmt.Errorf("%w: key_path empty", ErrMissingAuth)
		}
		data, err := os.ReadFile(cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("read key: %w", err)
		}
		var signer ssh.Signer
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(data)
		}
		if err != nil {
			return nil, fmt.Errorf("parse key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case "ssh_agent", "":
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, fmt.Errorf("%w: SSH_AUTH_SOCK not set", ErrMissingAuth)
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("dial agent: %w", err)
		}
		ag := agent.NewClient(conn)
		return []ssh.AuthMethod{ssh.PublicKeysCallback(ag.Signers)}, nil
	default:
		return nil, fmt.Errorf("%w: unknown auth_method %q", ErrMissingAuth, cfg.AuthMethod)
	}
}

// buildHostKeyCallback 根据 cfg.HostKey 选择 hostkey 校验策略。
//   - "insecure" → 跳过校验（仅用于本地测试，UI 提示风险）
//   - 其他       → 用系统 known_hosts；当前实现也是 insecure（M4 内）
//     M8 接 known_hosts 文件校验。
func buildHostKeyCallback(cfg config.Server) ssh.HostKeyCallback {
	if cfg.HostKey == "insecure" {
		return ssh.InsecureIgnoreHostKey()
	}
	// TODO(M8): 解析 ~/.ssh/known_hosts，使用 knownhosts.New。
	return ssh.InsecureIgnoreHostKey()
}
