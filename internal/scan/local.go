package scan

import (
	"context"

	"autoportforward/internal/model"
)

// ScanLocal 调用本机 `sonar list --json` 获取本地监听端口。
// TODO(M3): exec.CommandContext("sonar", "list", "--json") + json.Unmarshal 到中间结构 → []LocalPort。
// TODO(M3): sonar 不存在或失败时返回 error，上层降级为空集合。
func ScanLocal(ctx context.Context) ([]model.LocalPort, error) {
	_ = ctx
	return nil, nil
}
