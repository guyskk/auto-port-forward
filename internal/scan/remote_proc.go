package scan

import "autoportforward/internal/model"

// ParseProcNetTCP 解析 /proc/net/tcp (v6=false) 或 /proc/net/tcp6 (v6=true)。
//
// 文件每行形如：
//
//	sl  local_address     rem_address    st ...
//	0:  0100007F:A24C     00000000:0000  0A ...
//
// st == "0A" 表示 LISTEN；local_address 是 "HEX_IP:HEX_PORT"，端口大端十六进制；
// IPv4 IP 每 2 hex 字节为一个八位组并整体逆序（小端）；IPv6 类似但按 4 字节组逆序。
// 进程名 /proc/net/tcp 无法获取（只有 inode），降级时 Process 留空。
//
// TODO(M2): 实现 — 跳表头 / 畸形行容错 / IPv4 与 IPv6 字节序 / dual stack 处理。
func ParseProcNetTCP(out []byte, v6 bool) []model.RemotePort {
	_ = out
	_ = v6
	return nil
}
