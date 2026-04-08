package outbound

import (
	"net"
	"sync"
	"time"

	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/config"
)

// pooledConn 包装了连接及其出生时间，用于计算空闲超时
type pooledConn struct {
	conn      net.Conn
	createdAt time.Time
}

// ConnectionPool 管理后端连接预热池
type ConnectionPool struct {
	mu     sync.RWMutex
	pools  map[string]chan *pooledConn
	config *config.Config
	dialer *Dialer
}

// NewConnectionPool 构造函数
func NewConnectionPool(cfg *config.Config, dialer *Dialer) *ConnectionPool {
	return &ConnectionPool{
		pools:  make(map[string]chan *pooledConn),
		config: cfg,
		dialer: dialer,
	}
}

// PreheatAll 启动所有路由的预热子协程
func (p *ConnectionPool) PreheatAll() {
	for name, rule := range p.config.Routes {
		if rule.JumpStart > 0 {
			logger.Infof("🌡️ [预热启动] 正在为路由 [%s] -> %s 预生产 %d 个链接", name, rule.Addr, rule.JumpStart)
			p.initSubPool(name, rule)
		}
	}
}

func (p *ConnectionPool) initSubPool(name string, rule config.RouteRule) {
	p.mu.Lock()
	if _, ok := p.pools[rule.Addr]; ok {
		p.mu.Unlock()
		return
	}
	// 创建一个带缓冲的 channel 作为池子
	p.pools[rule.Addr] = make(chan *pooledConn, rule.JumpStart*2)
	p.mu.Unlock()

	// 启动补货协程
	go p.replenishLoop(rule)
}

// Get 尝试从池中获取连接，如果池空或链接失效则返回 nil
func (p *ConnectionPool) Get(addr string, timeoutSec int) net.Conn {
	p.mu.RLock()
	ch, ok := p.pools[addr]
	p.mu.RUnlock()

	if !ok {
		return nil
	}

	for {
		select {
		case pc := <-ch:
			// 1. 检查空闲超时
			if timeoutSec > 0 && time.Since(pc.createdAt) > time.Duration(timeoutSec)*time.Second {
				logger.Debugf("🍂 [池化清理] 链接已超过空闲时间 (%ds)，丢弃: %s", timeoutSec, addr)
				pc.conn.Close()
				continue
			}

			// 2. 极速健康检查 (非阻塞 Read)
			pc.conn.SetReadDeadline(time.Now().Add(time.Millisecond))
			one := make([]byte, 1)
			if _, err := pc.conn.Read(one); err != nil {
				// 如果是超时错误，说明链接还活着（因为没读到数据但不代表断了）
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					pc.conn.SetReadDeadline(time.Time{}) // 恢复 deadline
					return pc.conn
				}
				// 其他错误说明链接已死
				logger.Debugf("💀 [池化失效] 探测到链接已断开，丢弃: %s", addr)
				pc.conn.Close()
				continue
			}
			// 如果读到了数据（对于代理后端来说很奇怪），也认为不可信
			pc.conn.Close()
		default:
			return nil
		}
	}
}

// Acquire 是外部调用的主入口：优先从池中获取，拿不到则现场拨号
func (p *ConnectionPool) Acquire(rule config.RouteRule) (net.Conn, error) {
	conn := p.Get(rule.Addr, rule.IdleTimeout)
	if conn != nil {
		logger.Debugf("🔋 [池化命中] 使用预热链接: %s", rule.Addr)
		return conn, nil
	}
	
	logger.Debugf("🌬️ [池化穿透] 现场拨号: %s", rule.Addr)
	return p.dialer.Dial(rule)
}

// replenishLoop 后台补货泵
func (p *ConnectionPool) replenishLoop(rule config.RouteRule) {
	p.mu.RLock()
	ch := p.pools[rule.Addr]
	p.mu.RUnlock()

	// 初始填充
	for i := 0; i < rule.JumpStart; i++ {
		p.dialAndPut(ch, rule)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 如果池子不满，继续补货
		for len(ch) < rule.JumpStart {
			if !p.dialAndPut(ch, rule) {
				time.Sleep(5 * time.Second) // 拨号失败慢点充
				break
			}
		}
	}
}

func (p *ConnectionPool) dialAndPut(ch chan *pooledConn, rule config.RouteRule) bool {
	conn, err := p.dialer.Dial(rule)
	if err != nil {
		logger.Errorf("❌ [池化预热失败] 无法连接到后端 %s: %v", rule.Addr, err)
		return false
	}
	
	select {
	case ch <- &pooledConn{conn: conn, createdAt: time.Now()}:
		return true
	default:
		conn.Close()
		return false
	}
}
