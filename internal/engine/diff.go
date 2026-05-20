package engine

// DiffOp 描述一次 desired → current 的迁移指令。
type DiffOp struct {
	Kind string // "add" | "del"
	Port int
}

// Diff 计算从 current 到 desired 的迁移操作集合：
//
//	desired - current = add
//	current - desired = del
//
// 输入允许有重复元素，自动 dedupe。返回顺序不保证。
func Diff(current, desired []int) []DiffOp {
	cur := toSet(current)
	des := toSet(desired)
	var ops []DiffOp
	for p := range des {
		if _, ok := cur[p]; !ok {
			ops = append(ops, DiffOp{Kind: "add", Port: p})
		}
	}
	for p := range cur {
		if _, ok := des[p]; !ok {
			ops = append(ops, DiffOp{Kind: "del", Port: p})
		}
	}
	return ops
}

func toSet(xs []int) map[int]struct{} {
	m := make(map[int]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}
