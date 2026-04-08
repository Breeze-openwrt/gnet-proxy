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

### 🚚 双向透明转发 (Relay Chain)
为了平衡性能与开发难度，本方案采用了 **管道背压 (Backpressure)** 处理：
- 设置固定深度的 `writeChan` (128)。
- 当后端发送数据过慢导致管道堆积时，主动断开客户端连接，防止内存无限膨胀。

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
