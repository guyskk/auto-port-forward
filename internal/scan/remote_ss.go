package scan

import "autoportforward/internal/model"

// ParseSS 解析 `ss -H -tlnp` 或 `ss -H -tln` 的输出为 RemotePort 列表。
//
// 输入格式示例：
//
//	LISTEN 0      511        127.0.0.1:41548 0.0.0.0:* users:(("node",pid=20703,fd=50))
//	LISTEN 0      4096         0.0.0.0:9308  0.0.0.0:*
//	LISTEN 0      128            [::]:22     [::]:*    users:(("sshd",pid=1,fd=3))
//
// 算法概要：strings.Fields 切列；字段 0 必须为 "LISTEN"；字段 4 = local；
// 从右起最后一个 ':' 分割 host/port；users 段用正则提取 (proc, pid)。
//
// 该函数是纯函数 — 同输入恒等输出，方便单测。
//
// TODO(M2): 实现 — 处理 [::]:22 / *:3000 / [::ffff:127.0.0.1]:8080 / 缺 users / 空输入。
func ParseSS(out []byte) []model.RemotePort {
	_ = out
	return nil
}
