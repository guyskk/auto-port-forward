package sshcfg

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Resolve 调 `ssh -G <alias>` 取 effective 配置，提取 hostname/user/port。
//
// 输出格式：每行 "key value"（key 全小写）。其他字段忽略。
// port 解析失败或缺失 → 默认 22。
func Resolve(ctx context.Context, runner Runner, alias string) (Host, error) {
	if alias == "" {
		return Host{}, fmt.Errorf("sshcfg.Resolve: empty alias")
	}
	out, err := runner.Run(ctx, "ssh", "-G", alias)
	if err != nil {
		return Host{}, fmt.Errorf("ssh -G %s: %w", alias, err)
	}
	h := Host{Alias: alias, Port: 22}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		value := fields[1]
		switch key {
		case "hostname":
			h.HostName = value
		case "user":
			h.User = value
		case "port":
			if p, perr := strconv.Atoi(value); perr == nil && p > 0 {
				h.Port = p
			}
		}
	}
	return h, nil
}
