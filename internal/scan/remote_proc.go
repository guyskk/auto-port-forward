package scan

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"net"
	"strconv"
	"strings"

	"autoportforward/internal/model"
)

// ParseProcNetTCP 解析 /proc/net/tcp (v6=false) 或 /proc/net/tcp6 (v6=true)。
//
// 行格式：
//
//	sl  local_address     rem_address    st ...
//	 0: 0100007F:A24C     00000000:0000  0A ...
//
// st == "0A" 表示 LISTEN；local_address = "HEX_IP:HEX_PORT"，端口大端十六进制；
// IPv4 IP 每 2 hex 字节为一个八位组并整体逆序（小端）；
// IPv6 IP 32 hex 字符 = 16 字节，需要按每 4 字节小端逆序后拼接。
//
// 进程信息 /proc/net/tcp 拿不到（只有 inode），Process/PID 保持零值。
func ParseProcNetTCP(out []byte, v6 bool) []model.RemotePort {
	if len(out) == 0 {
		return nil
	}
	var rows []model.RemotePort
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1024*64), 1024*512)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// 至少需要 4 列：sl local rem st …
		if len(fields) < 4 {
			continue
		}
		if !strings.HasSuffix(fields[0], ":") {
			// 表头/异常行：第一列不是 "0:" / "12:" 样。
			continue
		}
		if fields[3] != "0A" {
			continue
		}
		ip, port, ok := parseProcLocal(fields[1], v6)
		if !ok {
			continue
		}
		ver := "IPv4"
		if v6 {
			ver = "IPv6"
		}
		rows = append(rows, model.RemotePort{
			Port:      port,
			BindAddr:  ip,
			IPVersion: ver,
		})
	}
	return rows
}

// parseProcLocal 解析形如 "0100007F:A24C" 的字段。
func parseProcLocal(field string, v6 bool) (string, int, bool) {
	i := strings.IndexByte(field, ':')
	if i <= 0 || i == len(field)-1 {
		return "", 0, false
	}
	ipHex := field[:i]
	portHex := field[i+1:]
	port, err := strconv.ParseInt(portHex, 16, 32)
	if err != nil {
		return "", 0, false
	}
	ipStr, ok := decodeProcIP(ipHex, v6)
	if !ok {
		return "", 0, false
	}
	return ipStr, int(port), true
}

// decodeProcIP 把 /proc/net/tcp 里的 hex IP 字符串解码。
//   - IPv4: 8 hex 字符 = 4 字节，整体小端 → 字节倒序后即标准 IPv4。
//     例: 0100007F → bytes(0x01,0x00,0x00,0x7F) → reverse → 7F,00,00,01 → 127.0.0.1
//   - IPv6: 32 hex 字符 = 16 字节，每 4 字节一组小端 → 组内倒序后即标准 IPv6。
//     例: 00000000000000000000000001000000 → group4 0x01000000 → 0x00000001 → 末位 ::1
func decodeProcIP(s string, v6 bool) (string, bool) {
	raw, err := hex.DecodeString(s)
	if err != nil {
		return "", false
	}
	if !v6 {
		if len(raw) != 4 {
			return "", false
		}
		reverse(raw)
		return net.IP(raw).String(), true
	}
	if len(raw) != 16 {
		return "", false
	}
	// 按 4 字节组小端解释。
	for i := 0; i < 16; i += 4 {
		reverse(raw[i : i+4])
	}
	return net.IP(raw).String(), true
}

func reverse(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}
