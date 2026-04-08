package sniffer

import (
	"encoding/binary"
	"testing"
)

// buildClientHello 构造一个合法的 TLS ClientHello 字节流用于测试
func buildClientHello(sni string) []byte {
	// SNI Extension
	sniBytes := []byte(sni)
	sniLen := uint16(len(sniBytes))
	
	// ServerNameList: NameType(1) + NameLen(2) + Name(n)
	sniExtData := make([]byte, 3+sniLen)
	sniExtData[0] = 0 // host_name
	binary.BigEndian.PutUint16(sniExtData[1:], sniLen)
	copy(sniExtData[3:], sniBytes)
	
	// ServerName Extension: Type(2) + TotalLen(2) + ListLen(2) + List(n)
	extTotalLen := uint16(len(sniExtData)) + 2
	ext := make([]byte, 4+extTotalLen)
	binary.BigEndian.PutUint16(ext[0:], 0x00) // Type: SNI
	binary.BigEndian.PutUint16(ext[2:], extTotalLen)
	binary.BigEndian.PutUint16(ext[4:], uint16(len(sniExtData)))
	copy(ext[6:], sniExtData)
	
	// Handshake: Version(2) + Random(32) + ID(1) + Cipher(2) + Comp(1) + ExtLen(2) + Exts
	// 偏移计算：2 + 32 + 1 + 2 + 1 = 38
	handshakeBody := make([]byte, 2+32+1+2+1+2+len(ext))
	handshakeBody[0], handshakeBody[1] = 0x03, 0x03 // TLS 1.2
	binary.BigEndian.PutUint16(handshakeBody[38:], uint16(len(ext)))
	copy(handshakeBody[40:], ext)
	
	// Handshake Header: Type(1) + Len(3)
	handshake := make([]byte, 4+len(handshakeBody))
	handshake[0] = 0x01 // ClientHello
	hLen := len(handshakeBody)
	handshake[1] = byte(hLen >> 16)
	handshake[2] = byte(hLen >> 8)
	handshake[3] = byte(hLen)
	copy(handshake[4:], handshakeBody)
	
	// Record Header: Type(1) + Ver(2) + Len(2)
	record := make([]byte, 5+len(handshake))
	record[0] = 0x16 // Handshake
	record[1], record[2] = 0x03, 0x01
	binary.BigEndian.PutUint16(record[3:], uint16(len(handshake)))
	copy(record[5:], handshake)
	
	return record
}

func TestParseSNI(t *testing.T) {
	tests := []struct {
		name    string
		sni     string
		raw     []byte
		wantErr bool
	}{
		{
			name: "Google",
			sni:  "google.com",
			raw:  buildClientHello("google.com"),
		},
		{
			name: "Baidu",
			sni:  "baidu.com",
			raw:  buildClientHello("baidu.com"),
		},
		{
			name:    "Invalid",
			raw:     []byte("GET / HTTP/1.1"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSNI(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSNI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.sni {
				t.Errorf("ParseSNI() = %v, want %v", got, tt.sni)
			}
		})
	}
}
