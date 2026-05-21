package sshctl

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// macOS unix socket 路径最大 104 字节；我们留 4 字节余量，所以目标 ≤ 100。
const maxSocketPath = 100

// SocketPath 返回 controlDir 下针对 alias 的 master socket 完整路径。
//
//   - 别名安全化：非 [A-Za-z0-9._-] 字符替换为 `_`。
//   - 若完整路径长度超过 maxSocketPath，则在 sanitized 名后加 `_` + 8 位 sha1 后缀，
//     并按需截断前缀，保证完整路径 ≤ maxSocketPath。
//
// 结果对相同输入是确定的（sha1 决定）。
func SocketPath(controlDir, alias string) string {
	safe := sanitize(alias)
	full := filepath.Join(controlDir, safe+".sock")
	if len(full) <= maxSocketPath {
		return full
	}
	// 后缀格式: `_` + 8 位 hex + `.sock` = 14 字符
	h := sha1.Sum([]byte(alias))
	suffix := "_" + hex.EncodeToString(h[:4]) + ".sock"

	// controlDir + 路径分隔符 + prefix + suffix ≤ maxSocketPath
	available := maxSocketPath - len(controlDir) - len(string(filepath.Separator)) - len(suffix)
	if available < 1 {
		available = 1
	}
	if len(safe) > available {
		safe = safe[:available]
	}
	return filepath.Join(controlDir, safe+suffix)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}
