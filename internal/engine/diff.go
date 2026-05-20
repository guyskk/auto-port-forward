package engine

// DiffOp 描述一次 desired → current 的迁移指令。
type DiffOp struct {
	Kind string // "add" | "del" | "noop"
	Port int
}

// Diff 计算从 current 到 desired 的迁移操作集合。
// 输入参数都是 int 端口集合，纯函数无 I/O。
//
// TODO(M5): 实现 — desired - current = add；current - desired = del；交集 = noop（可省略输出，仅 add/del）。
func Diff(current, desired []int) []DiffOp {
	_ = current
	_ = desired
	return nil
}
