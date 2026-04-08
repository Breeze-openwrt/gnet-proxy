package outbound

import (
	"net"
	"sync"
	"time"

	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/config"
)

/**
 * 🍱 [池化逻辑包装]
 * 并不是直接存 net.Conn，而是带上“出生证”，方便知道这个连接在池子里躺了多久。
 */
type pooledConn struct {
	conn      net.Conn  // 真实的 TCP 链路
	createdAt time.Time // 记录连接建立的时刻
}

/**
 * 🔋 [连接池大脑]：ConnectionPool
 * 核心思想：兵马未动，粮草先行。
 * 在请求还没来之前，先跟后端（如 Reality/Sing-box）打通一定数量的连接。
 * 这样当用户发起请求时，我们直接把通了的电筒“递”过去，跳过了经典的 TCP 三次握手过程。
 */
type ConnectionPool struct {
	mu     sync.RWMutex                  // 读写锁，保证多个协程同时取连接时不打架
	pools  map[string]chan *pooledConn   // 每个后端地址对应一个“仓库”（管道）
	config *config.Config                // 全局配置
	dialer *Dialer                       // 执行真实的物理连接拨号
}

// NewConnectionPool：构造函数
func NewConnectionPool(cfg *config.Config, dialer *Dialer) *ConnectionPool {
	return &ConnectionPool{
		pools:  make(map[string]chan *pooledConn),
		config: cfg,
		dialer: dialer,
	}
}

/**
 * 🌡️ PreheatAll：启动所有后端的温压弹。
 * 遍历配置文件里的每一条路由，如果设置了 JumpStart（预热链接数），
 * 就给这个后端开辟一个专门的高速公路（SubPool）。
 */
func (p *ConnectionPool) PreheatAll() {
	for name, rule := range p.config.Routes {
		if rule.JumpStart > 0 {
			logger.Infof("🌡️ [预热启动] 正在为路由 [%s] -> %s 预生产 %d 个链接", name, rule.Addr, rule.JumpStart)
			p.initSubPool(name, rule)
		}
	}
}

// initSubPool：为具体的某个地址初始化仓库
func (p *ConnectionPool) initSubPool(name string, rule config.RouteRule) {
	p.mu.Lock()
	if _, ok := p.pools[rule.Addr]; ok {
		p.mu.Unlock() // 防止重复初始化
		return
	}
	// 这里的管道深度设为 2 倍预热数，防止突发流量时池子瞬间被掏空
	p.pools[rule.Addr] = make(chan *pooledConn, rule.JumpStart*2)
	p.mu.Unlock()

	// 🛠️ [生产者协程]：启动一个后台“工厂”，源源不断地检查仓库水位并补货
	go p.replenishLoop(rule)
}

/**
 * 🔎 Get：从仓库里挑一个可用的链接
 */
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
			// ⏳ 1. 检查寿命：如果在仓库里躺太久了（超过 IdleTimeout），可能已经被防火墙掐死了
			if timeoutSec > 0 && time.Since(pc.createdAt) > time.Duration(timeoutSec)*time.Second {
				logger.Debugf("🍂 [池化清理] 链接已失效(超时)，丢弃: %s", addr)
				pc.conn.Close()
				continue
			}

			// 🩺 2. 极速体检：这一招非常精妙！
			// 设置一个 1 毫秒的读取时限，尝试读 1 个字节。
			// 如果读取报错且报的是“超时”，说明链路上没数据（这是正常的），同时也证明链接还没断！
			pc.conn.SetReadDeadline(time.Now().Add(time.Millisecond))
			one := make([]byte, 1)
			if _, err := pc.conn.Read(one); err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// ✅ 恭喜，检查通过，这是一个依然强健的链接
					pc.conn.SetReadDeadline(time.Time{}) // 恢复正常状态（去掉限制）
					return pc.conn
				}
				// 其他错误（如 EOF）说明后端主动断开或网络崩了
				logger.Debugf("💀 [池化失效] 探测到链接心跳停止，丢弃: %s", addr)
				pc.conn.Close()
				continue
			}
			// 如果居然读到了数据，说明这个链接被污染了（不应该有数据），也丢掉
			pc.conn.Close()
		default:
			// 仓库空了，只能告诉调用者“没存货了”
			return nil
		}
	}
}

/**
 * 🔋 Acquire：连接获取的主入口。
 * 就像买东西：先问仓库（池子）有没有库存，如果没有，立马现场生产一个（物理拨号）。
 */
func (p *ConnectionPool) Acquire(rule config.RouteRule) (net.Conn, error) {
	conn := p.Get(rule.Addr, rule.IdleTimeout)
	if conn != nil {
		logger.Debugf("🔋 [池化命中] 直接使用预热链接，秒开！: %s", rule.Addr)
		return conn, nil
	}
	
	logger.Debugf("🌬️ [池化穿透] 仓库告急，正在现场拨号: %s", rule.Addr)
	return p.dialer.Dial(rule)
}

/**
 * 🔄 replenishLoop：自动补货循环。
 * 这是一个永远运行在后台的勤劳协程。
 */
func (p *ConnectionPool) replenishLoop(rule config.RouteRule) {
	p.mu.RLock()
	ch := p.pools[rule.Addr]
	p.mu.RUnlock()

	// 1. 开机大吉：瞬间填满第一批预热链接
	for i := 0; i < rule.JumpStart; i++ {
		p.dialAndPut(ch, rule)
	}

	// 2. 细水长流：每 2 秒巡检一次
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 如果发现库存（管道长度）低于目标预热值，就开始补货
		for len(ch) < rule.JumpStart {
			if !p.dialAndPut(ch, rule) {
				// 拨号失败了（可能对方挂了），歇 5 秒再试，防止疯狂无效尝试导致 CPU 飙升
				time.Sleep(5 * time.Second) 
				break
			}
		}
	}
}

// dialAndPut：逻辑是将拨通的物理连接，打上出生日期标签，塞进仓库
func (p *ConnectionPool) dialAndPut(ch chan *pooledConn, rule config.RouteRule) bool {
	conn, err := p.dialer.Dial(rule)
	if err != nil {
		logger.Errorf("❌ [池化生产失败] 无法连通后端 %s: %v", rule.Addr, err)
		return false
	}
	
	select {
	case ch <- &pooledConn{conn: conn, createdAt: time.Now()}:
		return true
	default:
		// 如果这时候池子突然满了，就多退少补，直接关掉这个新链接防止溢出
		conn.Close()
		return false
	}
}
