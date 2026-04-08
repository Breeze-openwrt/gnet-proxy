# 🗺️ gnet-proxy 源码阅读导航

欢迎来到 `gnet-proxy` 的源码世界！这份文档旨在为“初中二年级编程基础”的你提供一张清晰的地图，让你能像读故事书一样读懂这套高性能代码。

## 🚦 读前准备：高性能编程的“三板斧”
在开始读代码之前，请记住本项目核心的三个加速秘诀：
1. **I/O 多路复用 (gnet)**：不再为每个连接开协程，而是一个线程管成千上万个连接。
2. **连接池 (Connection Pool)**：预先拨号，消除 TCP 三次握手的等待延迟。
3. **内存复用 (sync.Pool)**：借来的内存用完得还，减少垃圾回收（GC）的压力。

---

## 📅 故事线：流量的一生 (Flow Lifecycle)

按照以下顺序阅读文件，你将完整经历一个加密请求从进入到被转发的全过程：

### 1. 🚀 点火启动：程序的入口
- **文件**：[cmd/gnet-proxy/main.go](file:///d:/prj/mihhawork/gnet-proxy/cmd/gnet-proxy/main.go)
- **剧情**：
    - 读取你的配置文件 (`config.jsonc`)。
    - 组装各个部门（Router, Pool, Transport）。
    - 启动后台预热协程，先把跟后端（如 Xray）的电话拨通。
    - 开启 `inbound.Server` 等客上门。

### 2. 🚪 前台接待：新连接接入
- **文件**：[pkg/inbound/server.go](file:///d:/prj/mihhawork/gnet-proxy/pkg/inbound/server.go) -> `OnTraffic` 函数
- **剧情**：
    - 客户端连接进来，发送第一包数据（Client Hello）。
    - **第一步**：调用 `sniffer.ParseSNI` 提取流量想去的域名。
    - **第二步**：拿着域名去 `pkg/core/router.go` 问路。
    - **第三步**：如果匹配成功，立马派出一个异步小哥 (`go asyncDial`) 去连接池取链接，而不让主线程等。

### 3. 🕵️‍♂️ 侦探出马：识别域名
- **文件**：[pkg/common/sniffer/tls.go](file:///d:/prj/mihhawork/gnet-proxy/pkg/common/sniffer/tls.go)
- **剧情**：
    - 高效地在复杂的 TLS 协议里精准跳跃，只为找到那个明文域名。

### 4. 🔋 后勤部：连接池取件
- **文件**：[pkg/outbound/pool.go](file:///d:/prj/mihhawork/gnet-proxy/pkg/outbound/pool.go)
- **剧情**：
    - `asyncDial` 问连接池：“有没有现成的通往后端的连接？”。
    - 池子给出一个温热的连接，并做一次 1ms 的极速健康检查（体检）。

### 5. 🚚 搬运工：全速转发
- **文件**：[pkg/outbound/transport.go](file:///d:/prj/mihhawork/gnet-proxy/pkg/outbound/transport.go)
- **剧情**：
    - 连接建立后，开启“左右互搏”模式：
    - `relayUp`：把客户端的数据塞给后端。
    - `RelayBack`：把后端回来的数据全速丢给客户端。

---

## 🛠️ 模块索引 (Package Map)

| 目录 | 角色 | 核心职责 |
| :--- | :--- | :--- |
| `cmd/` | 启动器 | 完成依赖注入 (DI) 和程序初始化 |
| `pkg/config/` | 财务处 | 负责配置文件的定义与读取 |
| `pkg/core/` | 调度室 | 路由匹配逻辑 (精准、通配符、兜底) |
| `pkg/inbound/` | 营业厅 | 基于 gnet 的事件循环主引擎 |
| `pkg/outbound/` | 物流部 | 管理连接池、执行拨号、处理双向转发 |
| `pkg/common/` | 工具箱 | 包含嗅探器 (sniffer) 和内存池 (pool) |

---

## 💡 给读者的建议
- **不要害怕二进制**：在读 `tls.go` 时，配合 TLS 握手图解看更高效。
- **关注 `Context`**：在 `server.go` 中，`Context` 是连接的灵魂，记录了它所有的生命状态。
- **欣赏“异步”**：思考为什么我们要用 `go` 关键字开启协程。
