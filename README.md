# 🚀 gnet-proxy: 极速流量分发器

[![Go Release](https://github.com/Breeze-openwrt/gnet-proxy/actions/workflows/go.yml/badge.svg)](https://github.com/Breeze-openwrt/gnet-proxy/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

`gnet-proxy` 是一个专为 **高性能、轻量级、端口复用** 场景打造的流量分发引擎。它基于 `gnet` 极其高效的 Reactor 事件循环模型，实现了 TLS SNI 识别、多级路由匹配以及后端连接池预热技术。

> [!TIP]
> **适用场景**：REALITY 端口复用、域名流量审计、多后端（Xray/Sing-box/Mihomo）流量网关。

---

## ✨ 核心特性

- 🏎️ **极速性能**：基于 `gnet` v2 框架，采用单线程多路复用与 `sync.Pool` 内存池，吞吐量远超传统并发模型。
- 🌡️ **连接预热 (Pool)**：首创连接池预温技术。在请求到来前先建立后端链接，消除 TCP 三次握手延迟，实现 **请求秒开**。
- 🕵️‍♂️ **智能嗅探 (SNI)**：极速 TLS 握手解析，毫秒级提取访问域名，支持通配符、前缀及后缀匹配。
- 🛡️ **生产就绪**：内置守卫进程 (Daemon)、系统服务安装器、完善的日志追踪及优雅关闭机制。
- 🧩 **高可扩展**：遵循 DDD (领域驱动设计) 原则，逻辑与协议完全解耦。

---

## 🗺️ 源码阅读与学习

为了帮助开发者和初学者快速上手，我们提供了保姆级的文档体系：

1.  **[源码阅读导航](./docs/source_code_navigation.md)**：新手必看！跟随流量的一生，看清代码脉络。
2.  **[深度架构分析](./docs/architecture.md)**：进阶必看！深入理解 Reactor 模型、连接池及 DDD 设计理念。
3.  **[保姆级中文注释](./pkg/inbound/server.go)**：每一行代码都有详细解释，手把手教你高性能网络编程。

---

## 🛠️ 快速上手

### 1. 编译 (Build)
```bash
go build -o gnet-proxy ./cmd/gnet-proxy
```

### 2. 配置 (Config)
编辑 `config.jsonc` (支持 JSON 与注释)：
```jsonc
{
  "listen": "0.0.0.0:443",
  "multicore": true,
  "routes": {
    "google.com": { "addr": "127.0.0.1:10001", "jump_start": 5, "idle_timeout": 300 },
    "*.example.com": { "addr": "unix:/tmp/xray.sock" },
    "fallback": { "addr": "127.0.0.1:10002" }
  }
}
```

### 3. 运行 (Run)
```bash
# 前台运行并展示详细日志
./gnet-proxy -c config.jsonc -vv

# 安装为系统服务 (Windows)
./gnet-proxy install
```

---

## 📂 项目结构 (Structure)

```text
.
├── cmd/gnet-proxy/      # 入口：程序点火与依赖装配
├── pkg/
│   ├── inbound/         # 入站：基于 gnet 的高性能事件循环引擎
│   ├── outbound/        # 出站：连接池管理与全速转发 (Relay)
│   ├── core/            # 核心：路由分发大脑
│   ├── common/          # 公共：流量识别 (Sniffer) 与内存复用 (Pool)
│   └── config/          # 配置：多源配置加载与解析
└── docs/                # 文档：全方位的架构与源码解析
```

---

## ⚖️ 开源协议
本项目采用 [MIT License](./LICENSE) 许可协议。
