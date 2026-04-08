# 🛠️ gnet-proxy 运维与维护手册

这份文档旨在为 `gnet-proxy` 在 Linux 环境下的日常运维提供“傻瓜式”参考。

---

## 1. 常规生命周期管理

### 🚀 影子模式 (Daemon)
```bash
# 后台启动并自动清理旧实例
./gnet-proxy -c config.jsonc -d

# 查看是否启动成功 (看 PID 存不存在)
cat /run/gnet-proxy.pid
```

### 🛑 强力终止
如果因为极端情况需要手动暴力清理：
```bash
# 根据记录的 PID 杀
kill -9 $(cat /run/gnet-proxy.pid)

# 或按名称杀全家
pkill -9 gnet-proxy
```

### 🔄 平滑重启 (二次必启演示)
由于内置了 `EnforceSingleton` 逻辑，你只需要**再次执行带 `-d` 的命令**即可：
```bash
# 哪怕旧的在跑，直接再敲一遍，新的会自动顶替旧的
./gnet-proxy -c config.jsonc -d
```

---

## 2. 云原生集成 (Systemd)

如果你已经执行过 `./gnet-proxy install`，则可以通过系统标准指令管理：

*   **启动服务**：`systemctl start gnet-proxy`
*   **停止服务**：`systemctl stop gnet-proxy`
*   **查看状态**：`systemctl status gnet-proxy`
*   **查看系统日志**：`journalctl -u gnet-proxy -f`

---

## 3. 日志与排障

### 📈 日志追踪
日志文件默认位于 `/var/log/gnet-proxy.log`（可在配置文件修改）。
```bash
# 实时监控流量动向
tail -f /var/log/gnet-proxy.log
```

### 🔍 常见故障排查
- **`bind: address already in use`**：通常在非 `-d` 模式或无权限写入 `/run` 时发生。请确保以 `sudo` 运行或检查是否有残留进程未清理干净。
- **`Outbound Connection Timeout`**：表示后端服务（如 Xray）没启动或端口不通。请检查 `dialer.go` 中的保活时间。
- **流量断流**：检查日志是否有 `[背压预警]` 字样。如果有，说明你的下行客户端太慢，系统正在进行自我保护。

---

## 4. 总结
`gnet-proxy` 的设计原则是“免维护”。在正确安装后，它应该像系统内核一样静默、高效地在后台持续运行。
