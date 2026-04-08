package outbound

import (
	"net"
	"strings"
	"time"

	"gnet-proxy/pkg/config"
)

// Dialer 抽象化出站连接器机制
type Dialer struct{}

func NewDialer() *Dialer {
	return &Dialer{}
}

// Dial 负责进行真实的拨号并开启长连接支持
func (d *Dialer) Dial(rule config.RouteRule) (net.Conn, error) {
	network := "tcp"
	target := rule.Addr
	if strings.HasPrefix(target, "unix://") {
		network = "unix"
		target = strings.TrimPrefix(target, "unix://")
	} else if strings.HasPrefix(target, "tcp://") {
		network = "tcp"
		target = strings.TrimPrefix(target, "tcp://")
	}
	// 🛡️ [长连接保障] 使用自定义 Dialer 显式开启 TCP Keep-Alive
	dialer := &net.Dialer{
		Timeout:   3 * time.Second, // 拨号超时，超过 3 秒连不上后端就放弃，不让客户端傻等
		KeepAlive: 30 * time.Second, // 🆕 [铁律级保活]：每 30 秒发送一次心跳包，确保防火墙不会掐断闲置连接
	}
	conn, err := dialer.Dial(network, target)
	if err == nil && network == "tcp" {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
		}
	}
	return conn, err
}
