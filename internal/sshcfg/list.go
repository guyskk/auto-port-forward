package sshcfg

import (
	"context"
	"fmt"
	"strings"
)

// listAliasesScript 是 sh -c 跑的一行 pipeline：
//
//  1. 从 ~/.ssh/config 找以 Host<space> 开头的行（大小写不敏感，允许前导空白）。
//  2. awk 拆字段：从第 2 列起每个 token 单独输出（处理 "Host a b c" 多别名）。
//  3. 过滤通配符别名（含 * ? !）。
//  4. 按字典序去重，输出稳定。
//
// 找不到任何行时 grep/sort 返回 exit 1，Runner 实现把它当 "无匹配" 处理。
const listAliasesScript = `grep -iE '^[[:space:]]*Host[[:space:]]' "$HOME/.ssh/config" 2>/dev/null ` +
	`| awk '{for(i=2;i<=NF;i++) print $i}' ` +
	`| grep -vE '[*?!]' ` +
	`| sort -u`

// ListAliases 返回 ~/.ssh/config 中所有具体（非通配符）的 Host 别名。
//
// 顺序：按字典序（来自 pipeline 末尾的 sort -u）。
// 文件缺失、无匹配、grep exit code != 0 但 stdout 为空 → 返回 nil, nil。
func ListAliases(ctx context.Context, runner Runner) ([]string, error) {
	out, err := runner.Run(ctx, "sh", "-c", listAliasesScript)
	if err != nil && len(out) == 0 {
		// grep 无匹配（exit 1）或 file not found → 没有 host 可列。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list ssh aliases: %w", err)
	}
	var aliases []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		aliases = append(aliases, line)
	}
	return aliases, nil
}

// ListHosts 列举所有具体别名，再用 ssh -G 解析每个的 effective 配置。
//
// 单个 host 的 Resolve 失败不阻塞其他 host，但其本身被跳过。
func ListHosts(ctx context.Context, runner Runner) ([]Host, error) {
	aliases, err := ListAliases(ctx, runner)
	if err != nil {
		return nil, err
	}
	var hosts []Host
	for _, alias := range aliases {
		h, err := Resolve(ctx, runner, alias)
		if err != nil {
			continue
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}
