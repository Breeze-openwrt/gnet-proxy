# 🏗️ gnet-proxy 深度架构分析

这份文档深入探讨了 `gnet-proxy` 的设计理念、高性能背后的逻辑以及架构模式。

---

## 1. 核心架构模式：DDD (领域驱动设计)
本项目将业务逻辑拆分为三个核心层级，实现了逻辑的完全解耦：

### 🧱 领域模型 (Domain Model)
- **配置定义** (`pkg/config`): 业务规则的静态描述。
- **路由核心** (`pkg/core`): 决策系统的灵魂，不依赖任何网络框架，只负责算路由。

### 🏗️ 基础设施 (Infrastructure)
- **gnet** 入站引擎: 负责高性能的 I/O 多路复用。
- **内存池** (`pkg/common/pool`): 提供 `sync.Pool` 的字节切片复用，极大减轻 GC 负担。
- **日志系统**: 提供可落地的运行轨迹。

### 🚀 应用服务 (Application Service)
- **Server** (`pkg/inbound`): 将入站、路由、出站逻辑串联起来，协调整个流量转发过程。

---

## 2. 并发与转发模型：极致性能的秘诀

### 🚄 I/O 多路复用 (The Reactor Pattern)
传统的代理程序（如早期版本的 Shadowsocks 或简单的 Go 代理）通常采用 `accept -> goroutine -> write` 模式。当连接数达到数万时，协程上下文切换和堆栈开销会迅速增加。
`gnet-proxy` 基于 `gnet` 实现：
- **单线程多路复用**：一个事件线程可以同时监听处理数千个文件描述符。
- **非阻塞 I/O**：连接建立过程异步化，绝不阻塞主循环。

### 🔌 非阻塞连接池与健康检查
项目实现了自定义的 **异步填充连接池 (Asynchronous Connection Pool)**：
- **JumpStart (预先拨号)**：程序启动时即向后端发起 TCP 握手。
- **1ms 极速探测**：通过 `SetReadDeadline` 实现零延迟的连接存活检查，确保用户拿到的连接 100% 可用。

### 🚚 双向透明转发与智能背压 (Backpressure)
为了平衡性能与系统稳定性，本方案引入了 **双水位线动态流控 (Dynamic Flow Control)**：
- **高水位 (High Watermark, 80%)**：当 `writeChan` 积压超过了 **2048** 个插槽（约 **64MB**）的 80% (1600) 时，进入“读拦截”模式。通过 TCP 的窗口自适应机制（Window Zero），自然地让发送端（客户端）减速，**防止内存膨胀导致的 OOM**。
- **低水位 (Low Watermark, 20%)**：当后端消费了大部分积压数据，水位降至 20% (400) 以下时，从管道消费端调用 `gnet.Conn.Wake()` 唤醒事件循环并恢复读取。
- **下行背压控制 (Downlink Control)**：在 `RelayBack` 协程中持续监控 `OutboundBuffered`（待发缓冲区）。如果客户端由于带宽原因接收缓慢导致数据堆积超过 **8MB**，则自动挂起后端读取。这能反向触发远程服务器的 TCP 拥塞控制，实现了全链路、闭环的流量平衡。

---

## 3. 设计模式的应用 (Design Patterns)

### 🧩 依赖注入 (Dependency Injection)
在 `main.go` 中，你可以清晰地看到整个软件是如何“拼装”出来的。
```go
// 每一个组件都是解耦的，这使得测试非常简单
router := core.NewRouter(cfg.Routes)
pool := outbound.NewConnectionPool(cfg, dialer)
server := inbound.NewServer(..., router, ..., pool, ...)
```

### 🧠 状态机 (State Machine)
每个连接都有自己的 Context，通过 `isDialing` 和 `isProxying` 两个布尔值驱动不同的数据处理逻辑：
- `Unknown`: 刚刚接入，需要嗅探 SNI。
- `Dialing`: 正在向后端拨号，数据暂存在 `writeChan` 管道中。
- `Proxying`: 已经打通，数据全速双向转发。

---

## 4. 总结：设计者的初衷
`gnet-proxy` 不是一个全功能的梯子，而是一个 **高性能、可解耦的流量指挥官**。
它的设计哲学是：**轻量、极速、无侵入**。它致力于在 443 端口这个寸土寸金的地带，实现各种协议（Xray, Mihomo, Reality, etc.）的高效复用与共存。
