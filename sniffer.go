package main

import (
	"encoding/binary"
	"errors"
)

// 🧐 TLS 嗅探器：在不拆包、不解密的情况下，从字节流中偷看域名（SNI）。
// 这是“零内存分配 (Zero Allocation)”版本，追求极致性能。

var (
	ErrNotTLS           = errors.New("不是合法的 TLS 流量")
	ErrNoSNI            = errors.New("未发现 SNI 扩展字段")
	ErrIncompletePacket = errors.New("握手包不完整，需要更多数据")
)

// ParseSNI 接收原始字节流，返回解析出的域名。
// 💡 编程思想：我们像是在一堆乱码里根据“地图”寻找宝藏，只寻址，不产生多余的内存拷贝。
func ParseSNI(data []byte) (string, error) {
	// 1️⃣ 基础检查：TLS 记录头长度至少要 5 字节
	// [Type(1)] [Version(2)] [Length(2)]
	if len(data) < 5 {
		return "", ErrIncompletePacket
	}

	// 2️⃣ 确认是 TLS Handshake (类型 22)
	if data[0] != 0x16 {
		return "", ErrNotTLS
	}

	// 3️⃣ 获取 Handshake 消息的总长度
	recordLen := int(binary.BigEndian.Uint16(data[3:5]))
	if len(data) < 5+recordLen {
		return "", ErrIncompletePacket
	}

	// 4️⃣ 跳过 Record Header，进入 Handshake Body
	// Handshake Type (1) + Length (3) + Version (2) + Random (32)
	pos := 5
	if len(data) < pos+38 {
		return "", ErrIncompletePacket
	}

	// 确认是 Client Hello (类型 1)
	if data[pos] != 0x01 {
		return "", ErrNotTLS
	}
	pos += 4 // 跳过 Type 和 Length

	pos += 2  // 跳过 Version (如 TLS 1.2)
	pos += 32 // 跳过 Random 随机数

	// 5️⃣ 跳过 Session ID (长度 1 字节 + 内容)
	if len(data) < pos+1 {
		return "", ErrIncompletePacket
	}
	sessionIDLen := int(data[pos])
	pos += 1 + sessionIDLen

	// 6️⃣ 跳过 Cipher Suites (长度 2 字节 + 内容)
	if len(data) < pos+2 {
		return "", ErrIncompletePacket
	}
	cipherLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2 + cipherLen

	// 7️⃣ 跳过 Compression Methods (长度 1 字节 + 内容)
	if len(data) < pos+1 {
		return "", ErrIncompletePacket
	}
	compLen := int(data[pos])
	pos += 1 + compLen

	// 8️⃣ 终于到了 TLS 扩展字段 (Extensions)
	if len(data) < pos+2 {
		return "", ErrNoSNI
	}
	extensionTotalLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2
	end := pos + extensionTotalLen

	if len(data) < end {
		return "", ErrIncompletePacket
	}

	// 9️⃣ 在扩展字段中循环寻找 Server Name (类型 0)
	for pos+4 <= end {
		extensionType := binary.BigEndian.Uint16(data[pos : pos+2])
		extensionLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		pos += 4

		if extensionType == 0x00 { // 🎯 找到了！这就是 Server Name 扩展
			if len(data) < pos+extensionLen {
				return "", ErrNoSNI
			}
			// Server Name List Length (2) + Name Type (1) + Name Length (2)
			if extensionLen < 5 {
				return "", ErrNoSNI
			}
			nameLen := int(binary.BigEndian.Uint16(data[pos+3 : pos+5]))
			if len(data) < pos+5+nameLen {
				return "", ErrNoSNI
			}
			return string(data[pos+5 : pos+5+nameLen]), nil
		}
		pos += extensionLen
	}

	return "", ErrNoSNI
}
