package scan

import (
	"context"
	"errors"
	"os/exec"

	"autoportforward/internal/model"
)

// ErrSonarNotFound 表示本机未安装 sonar。
var ErrSonarNotFound = errors.New("sonar not found in PATH")

// ScanLocal 调用本机 `sonar list --json` 获取本地监听端口。
// sonar 不存在时返回 ErrSonarNotFound，上层降级为空集合。
func ScanLocal(ctx context.Context) ([]model.LocalPort, error) {
	bin, err := exec.LookPath("sonar")
	if err != nil {
		return nil, ErrSonarNotFound
	}
	out, err := exec.CommandContext(ctx, bin, "list", "--json").Output()
	if err != nil {
		return nil, err
	}
	return ParseSonarJSON(out)
}
