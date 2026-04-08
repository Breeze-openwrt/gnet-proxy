# 🍵 Gnet-Proxy 后端连接预热池实施计划

为了极致降低首包延迟（TTFB），我们将引入“精简连接池”方案。该方案的核心是通过**空闲预拨号**来消除 TCP 三路握手的往返时延（RTT）。

## ⚠️ User Review Required
> [!IMPORTANT]
> **逻辑核心确认**：在纯 L4 转发中，由于协议状态（如 TLS 握手）是随流而动的，我们无法像 HTTP 1.1 那样复用已经发送过数据的链接。因此，本方案是 **“预拨号池”**：预先建立好基础 TCP 链接，客户端进来时拿走一个“新鲜”的链接使用，使用完即关闭。同时后台自动补充新链接。

## Proposed Changes

### 1. [配置层] 引入预热参数 (`pkg/config/config.go`)
- 在 `RouteRule` 结构体中增加 `jump_start` (预热链接数) 字段。
- 示例：`"updates.cdn-apple.com": { "addr": "...", "jump_start": 4 }`。

### 2. [出站层] 实现连接池管理器 (`pkg/outbound/pool.go`) [NEW]
- 使用 `map[string]chan net.Conn` 为每个目标地址维护一个独立队列。
- **后台填充协程 (Replenisher)**：持续监控 channel 长度，一旦低于预设值，立即启动异步 `Dial` 补货。
- **存活校验**：在从池中取出链接时，进行极速的 `Read` 探测，剔除已经被后端超时断开的“死鏈”。

### 3. [适配器层] 修改 Dialer 调用逻辑 (`pkg/outbound/dialer.go`)
- 改造 `Dial` 方法：优先尝试从 `Pool` 中抢夺链接。
- 如果池子为空（瞬时并发极高），则降级为实时 `Dial`。

### 4. [生命周期] 预热触发器 (`cmd/gnet-proxy/main.go`)
- 在 `main` 函数启动后，异步调用 `pool.PreheatAll()`，让后端链接在流量到来前就处于“待命”状态。

## Open Questions
> [!TIP]
> **关于链接清理**：如果長期没有流量，池子里的链接会一直占用后端 FD。是否需要设置一个 `idle_timeout`（例如 5 分鐘），超時後自動排空池子？

## Verification Plan
1. **延迟对比**: 使用 `tcping` 或 `curl` 测试，观察首包返回时间。预热模式下应减少 1 个 RTT。
2. **连接监控**: 使用 `ss -nt` 观察到后端（127.0.0.1:10443）始终保持有 N 个 `ESTABLISHED` 状态的空闲链接。
3. **鲁棒性测试**: 强杀后端程序，观察 `gnet-proxy` 能否正确清理失效链接并自动重连。
