package sniffer

import (
	"errors" // 错误处理工具
)

/**
 * 🕵️‍♂️ [流量侦探：TLS 协议拆分器]
 * TLS 是互联网安全的基石。当你在浏览器输入 https://google.com 时，
 * 你的电脑发出的第一个包叫 "Client Hello"。
 * 这个文件唯一的任务就是：从这个复杂的加密包里，抠出那个没加密的“域名”字段（SNI）。
 */

// 定义一些通用的错误提示
var (
	ErrNotTLS           = errors.New("不是标准的 TLS 流量")
	ErrIncompletePacket = errors.New("TCP 数据包不完整，还没攒够识别所需的数据")
)

/**
 * ParseSNI：从二进制数据流中提取 SNI 域名
 * 这里的逻辑极其硬核且高效，通过对 TLS 字节协议的精准跳跃，跳过无关的字段。
 */
func ParseSNI(data []byte) (string, error) {
	// 🏠 1. 基础门槛检查
	// TLS 数据包的头 5 个字节是它的“名片”。
	// 第 1 字节必须是 0x16 (代表握手包 Handshake)
	// 第 2-3 字节是版本 (如 TLS 1.2)
	if len(data) < 5 {
		return "", ErrIncompletePacket
	}
	if data[0] != 0x16 {
		return "", ErrNotTLS
	}

	// 📏 2. 获取整个 TLS 报文的总长度
	totalLen := int(data[3])<<8 | int(data[4])
	if len(data) < totalLen+5 {
		// 说明网络还在传输中，数据还没收齐
		return "", ErrIncompletePacket
	}

	// 🕵️‍♂️ 3. 进入 Handshake 协议层 (第 6 字节开始)
	// 第 1 字节必须是 0x01 (代表 Client Hello 类型)
	pos := 5
	if data[pos] != 0x01 {
		return "", ErrNotTLS
	}

	// 🏃‍♂️ 极速跳跃逻辑：
	// 我们需要跳过 Session ID、Cipher Suites、Compression Methods 等大块内容。
	// 这里通过精准计算长度偏移来实现跨越。

	pos += 4 // 跳过总包头
	pos += 2 // 跳过版本号
	pos += 32 // 跳过随机数 (Random Bytes)

	// 跳过 Session ID (变长字段)
	if len(data) <= pos { return "", ErrNotTLS }
	sessionIDLen := int(data[pos])
	pos += 1 + sessionIDLen

	// 跳过 Cipher Suites (变长字段)
	if len(data) <= pos+1 { return "", ErrNotTLS }
	cipherSuiteLen := int(data[pos])<<8 | int(data[pos+1])
	pos += 2 + cipherSuiteLen

	// 跳过 Compression Methods (变长字段)
	if len(data) <= pos { return "", ErrNotTLS }
	compressionLen := int(data[pos])
	pos += 1 + compressionLen

	// --- 到达核心地带：Extensions (扩展字段) ---
	if len(data) <= pos+1 { return "", ErrNotTLS }
	extensionsLen := int(data[pos])<<8 | int(data[pos+1])
	pos += 2
	
	end := pos + extensionsLen
	if len(data) < end {
		return "", ErrNotTLS
	}

	// 🔍 在扩展字段里“大海捞针”，寻找 SNI (Type = 0)
	for pos+4 <= end {
		extType := int(data[pos])<<8 | int(data[pos+1])
		extLen := int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4

		if extType == 0 { // 找到啦！SNI 类型的扩展
			// SNI 内部还包裹了一层 List 和 Name Type (0 代表 host_name)
			if pos+5 > end { return "", ErrNotTLS }
			serverNameListLen := int(data[pos])<<8 | int(data[pos+1])
			if pos+2+serverNameListLen > end { return "", ErrNotTLS }
			
			// 最终提取出域名字符串
			nameType := data[pos+2]
			if nameType == 0 {
				nameLen := int(data[pos+3])<<8 | int(data[pos+4])
				if pos+5+nameLen > end { return "", ErrNotTLS }
				return string(data[pos+5 : pos+5+nameLen]), nil
			}
		}
		pos += extLen // 没找到，跳到下一个扩展
	}

	return "", errors.New("流量中未包含 SNI 扩展字段")
}
