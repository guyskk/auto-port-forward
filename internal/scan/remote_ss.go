package scan

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"autoportforward/internal/model"
)

// usersRe 匹配 ss 的 `users:(("proc",pid=NNN,fd=NN), ...)` 段落中的首个进程名与 pid。
var usersRe = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// ParseSS 解析 `ss -H -tlnp` / `ss -H -tln` 的输出为 RemotePort 列表。
//
// 输入格式示例：
//
//	LISTEN 0      511        127.0.0.1:41548 0.0.0.0:* users:(("node",pid=20703,fd=50))
//	LISTEN 0      4096         0.0.0.0:9308  0.0.0.0:*
//	LISTEN 0      4096            [::]:22       [::]:*
//	LISTEN 0      4096               *:9097        *:* users:(("mihomo",pid=2401,fd=3))
//
// 该函数是纯函数 — 同输入恒等输出。
func ParseSS(out []byte) []model.RemotePort {
	if len(out) == 0 {
		return nil
	}
	var rows []model.RemotePort
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1024*64), 1024*512)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// 形如 `LISTEN 0 511 127.0.0.1:41548 0.0.0.0:* users:(...)`。
		// 至少需要 5 列：State Recv-Q Send-Q Local-Addr:Port Peer-Addr:Port。
		if len(fields) < 5 {
			continue
		}
		if fields[0] != "LISTEN" {
			continue
		}
		local := fields[3]
		host, port, ok := splitHostPort(local)
		if !ok {
			continue
		}
		bind, ipVer := normalizeBind(host)
		rp := model.RemotePort{
			Port:      port,
			BindAddr:  bind,
			IPVersion: ipVer,
		}
		// 进程信息可能跨剩余字段：取 fields[5:] 重新合并扫描 users:(...) 段。
		if len(fields) > 5 {
			tail := strings.Join(fields[5:], " ")
			if m := usersRe.FindStringSubmatch(tail); len(m) == 3 {
				rp.Process = m[1]
				if pid, err := strconv.Atoi(m[2]); err == nil {
					rp.PID = pid
				}
			}
		}
		rows = append(rows, rp)
	}
	return rows
}

// splitHostPort 从 ss 的 Local Address:Port 字段切分。
// 处理形态：127.0.0.1:80 / 0.0.0.0:80 / [::]:22 / [::1]:631 /
// [::ffff:127.0.0.1]:8080 / *:9097 / 127.0.0.53%lo:53。
func splitHostPort(s string) (host string, port int, ok bool) {
	if s == "" {
		return "", 0, false
	}
	// 带方括号：[host]:port。
	if strings.HasPrefix(s, "[") {
		idx := strings.LastIndex(s, "]:")
		if idx < 0 {
			return "", 0, false
		}
		host = s[1:idx]
		p, err := strconv.Atoi(s[idx+2:])
		if err != nil {
			return "", 0, false
		}
		return host, p, true
	}
	// 不带方括号：右起最后一个 ':'。
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", 0, false
	}
	host = s[:idx]
	p, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return "", 0, false
	}
	// 去掉 zone index (host%zone)。
	if zi := strings.Index(host, "%"); zi >= 0 {
		host = host[:zi]
	}
	return host, p, true
}

// normalizeBind 把 host 转成 (bindAddr, ipVersion)。
// `*` → ("*", "dual")  双栈
// `::` / `::1` / 含 ':' 的非映射地址 → IPv6
// 其他 → IPv4
func normalizeBind(host string) (string, string) {
	if host == "*" {
		return "*", "dual"
	}
	// 含 ':' 视为 IPv6 字面量（含 ::ffff:127.0.0.1 这种映射地址也算 IPv6 socket）。
	if strings.Contains(host, ":") {
		return host, "IPv6"
	}
	return host, "IPv4"
}
