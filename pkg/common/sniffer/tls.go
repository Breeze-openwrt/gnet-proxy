package sniffer

import (
	"errors"

	"golang.org/x/crypto/cryptobyte"
)

// 🧐 TLS 嗅探器：在不拆包、不解密的情况下，从字节流中偷看域名（SNI）。
// 🚢 工业级重构：采用 Go 官方底层包 golang.org/x/crypto/cryptobyte
// 彻底解决手写 offset 游标可能导致的数组越界 (Panic) 和非法包攻击，同时保持真正的零内存分配 (Zero-Allocation)。

var (
	ErrNotTLS           = errors.New("不是合法的 TLS 流量")
	ErrNoSNI            = errors.New("未发现 SNI 扩展字段")
	ErrIncompletePacket = errors.New("握手包不完整，需要更多数据")
)

// ParseSNI 接收原始字节流，返回解析出的域名。
func ParseSNI(data []byte) (string, error) {
	// 将原始切片包装为安全的 cryptobyte 读取器
	s := cryptobyte.String(data)

	// 1️⃣ [TLS Record Layer Type(1 Byte)]
	var recordType uint8
	if !s.ReadUint8(&recordType) {
		return "", ErrIncompletePacket
	}
	if recordType != 0x16 {
		return "", ErrNotTLS
	} // 必须是 Handshake (22)

	// 2️⃣ [TLS Version(2 Bytes)]
	if !s.Skip(2) {
		return "", ErrIncompletePacket
	}

	// 3️⃣ [TLS Record Length(2 Bytes)] + Body
	var record cryptobyte.String
	if !s.ReadUint16LengthPrefixed(&record) {
		return "", ErrIncompletePacket
	}

	// 4️⃣ [Handshake Type(1 Byte)]
	var handshakeType uint8
	if !record.ReadUint8(&handshakeType) {
		return "", ErrIncompletePacket
	}
	if handshakeType != 0x01 {
		return "", ErrNotTLS
	} // 必须是 ClientHello (1)

	// 5️⃣ [Handshake Length(3 Bytes)] + Body
	var clientHello cryptobyte.String
	if !record.ReadUint24LengthPrefixed(&clientHello) {
		return "", ErrIncompletePacket
	}

	// 跳过 Version (2 Bytes) 和 Random (32 Bytes)
	if !clientHello.Skip(2) || !clientHello.Skip(32) {
		return "", ErrIncompletePacket
	}

	// 跳过 Session ID (长度由前 1 字节决定)
	var sessionID cryptobyte.String
	if !clientHello.ReadUint8LengthPrefixed(&sessionID) {
		return "", ErrIncompletePacket
	}

	// 跳过 Cipher Suites (长度由前 2 字节决定)
	var cipherSuites cryptobyte.String
	if !clientHello.ReadUint16LengthPrefixed(&cipherSuites) {
		return "", ErrIncompletePacket
	}

	// 跳过 Compression Methods (长度由前 1 字节决定)
	var compressionMethods cryptobyte.String
	if !clientHello.ReadUint8LengthPrefixed(&compressionMethods) {
		return "", ErrIncompletePacket
	}

	// 如果没有扩展字段，提前返回
	if clientHello.Empty() {
		return "", ErrNoSNI
	}

	// 6️⃣ [Extensions Length(2 Bytes)] + Extensions 流
	var extensions cryptobyte.String
	if !clientHello.ReadUint16LengthPrefixed(&extensions) {
		return "", ErrIncompletePacket
	}

	// 7️⃣ 安全循环遍历所有 Extensions
	for !extensions.Empty() {
		var extType uint16
		var extData cryptobyte.String
		if !extensions.ReadUint16(&extType) || !extensions.ReadUint16LengthPrefixed(&extData) {
			return "", ErrIncompletePacket
		}

		// 🎯 找到了 Server Name (类型编号为 0x00)
		if extType == 0x00 {
			var nameList cryptobyte.String
			if !extData.ReadUint16LengthPrefixed(&nameList) {
				return "", ErrNoSNI
			}

			for !nameList.Empty() {
				var nameType uint8
				var serverName cryptobyte.String
				if !nameList.ReadUint8(&nameType) || !nameList.ReadUint16LengthPrefixed(&serverName) {
					return "", ErrNoSNI
				}
				if nameType == 0 { // 确认是 host_name (0)
					return string(serverName), nil
				}
			}
		}
	}

	return "", ErrNoSNI
}
