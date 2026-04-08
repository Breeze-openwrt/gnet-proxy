package core

import (
	"net"
	"gnet-proxy/pkg/config"
)

// 🧩 Inbound 代表一个入站适配器（如 gnet 服务器）
type Inbound interface {
	Run() error
	Close() error
}

// 🧩 Outbound 代表一个出站分发器（如 TCP 拨号器）
type Outbound interface {
	Dial(rule config.RouteRule) (net.Conn, error)
}

// 🧩 Context 记录单次代理会话的生命周期状态
type Context interface {
	Get(key string) interface{}
	Set(key string, val interface{})
}
