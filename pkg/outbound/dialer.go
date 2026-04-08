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
		Timeout:   3 * time.Second,
		KeepAlive: 5 * time.Minute, // 每 5 分钟发送一次保活探测包
	}
	return dialer.Dial(network, target)
}
