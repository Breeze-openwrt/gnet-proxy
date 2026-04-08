# 🚀 gnet-proxy: 极致性能 443 端口 SNI 分流转发引擎 (JSONC 版)

`gnet-proxy` 是一个专注于 **443 端口 SNI 分流** 的工业级极致性能工具。它利用 **gnet** 框架的多 Reactor 模型，在不解密 TLS 的情况下，将流量精准分发至后端的 Xray、Mihomo 或 Sing-box。

---

## ✨ 核心特性 (Key Features)

- **0 内存分配嗅探**: 极致精简的 TLS 解析器，不产生任何 GC 压力。
- **JSONC 原生支持**: 配置文件支持单行（`//`）和多行（`/* */`）注释。
- **零拷贝转发**: 使用 `sync.Pool` 池化缓冲区，吞吐量直逼物理极限。
- **智能 IP 透传**: 自动识别 TCP4/TCP6 PROXY Protocol，支持按域名精准开关。
- **UDS 转发支持**: 支持 `unix:///` 路径转发，本地通信延迟更低。

---

## 🚦 快速开始 (Quick Start)

### 📥 1. 编译
```bash
go build -o gnet-proxy .
```

### 📂 2. 配置 (`config.jsonc`)
程序默认读取当前目录下的 `config.jsonc`：
```jsonc
{
  "listen_addr": "[::]:443",        // 🌈 监听 IPv4/IPv6 双栈
  "multicore": true,                 // 🏎️ 性能全开
  "proxy_protocol": true,            // 全局开启 IP 透传
  "routes": {
    "google.com": {
      "addr": "tcp://127.0.0.1:10001",
      "proxy_protocol": true        // 🟢 开启透传 (Xray)
    },
    "apple.com": {
      "addr": "unix:///tmp/sb.sock",
      "proxy_protocol": false       // 🔴 显式关闭 (适配 sing-box 1.6.0+)
    }
  }
}
```

### 🚀 3. 运行命令
```bash
# 👻 后台守护运行 (默认加载 config.jsonc)
./gnet-proxy -d

# 🔍 详细调试运行
./gnet-proxy -vvv

# 🛠️ 指定其他配置文件
./gnet-proxy -c custom.jsonc
```

---

## 🚦 命令行标志 (Flags)

| 标志 | 说明 |
| :--- | :--- |
| **`-c`** | 指定 JSONC 配置文件路径 (默认: `config.jsonc`) |
| **`-d`** | 后台运行 (Daemon 模式) |
| **`-v`** | 增加日志详细程度 (-v, -vv, -vvv) |
| **`-h`** | 查看帮助手册 |

---

## 🛡️ 生产环境建议 (Production)

- **权限管理**: 推荐赋予二进制文件 `setcap` 权限以监听 443 端口：
  `sudo setcap 'cap_net_bind_service=+ep' ./gnet-proxy`
- **日志管理**: 生产环境建议使用 `-v` 基础日志，并将 `log_file` 指向固定的日志目录。

**极致性能，从这一刻开始。**
