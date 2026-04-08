#!/bin/bash

# =================================================================
# 🚀 gnet-proxy 极速一键安装脚本 (Linux amd64 专属版)
# =================================================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=====================================================${NC}"
echo -e "${BLUE}   🛡️  gnet-proxy 工业级高性能转发器一键安装程序        ${NC}"
echo -e "${BLUE}=====================================================${NC}"

# 1. 环境自检
if [[ "$(uname -s)" != "Linux" ]] || [[ "$(uname -m)" != "x86_64" ]]; then
    echo -e "${RED}❌ 错误: 本脚本仅支持 Linux amd64 平台。${NC}"
    exit 1
fi

if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}❌ 错误: 必须使用 root 权限运行此脚本 (使用 sudo)。${NC}"
   exit 1
fi

# 2. 从 GitHub 获取最新版本
REPO="Breeze-openwrt/gnet-proxy"
echo -e "🔍 正在从 GitHub 获取最新版本信息..."
LATEST_TAG=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
    echo -e "${RED}❌ 无法获取最新版本号，请检查网络连接。${NC}"
    exit 1
fi

echo -e "✅ 发现最新版本: ${GREEN}$LATEST_TAG${NC}"

# 3. 下载二进制文件
FILENAME="gnet-proxy-linux-amd64"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/$FILENAME"

echo -e "📥 正在下载二进制文件: $FILENAME..."
curl -L -o /tmp/gnet-proxy "$DOWNLOAD_URL"

chmod +x /tmp/gnet-proxy
echo -e "✅ 下载完成并已赋予执行权限。"

# 4. 执行二进制内置的工业级安装程序
echo -e "⚙️  正在执行内置安装流程 (Systemd 集成)..."
/tmp/gnet-proxy install

# 5. 清理与收尾
rm /tmp/gnet-proxy
echo -e "${GREEN}=====================================================${NC}"
echo -e "${GREEN}🎉 安装圆满成功！gnet-proxy 已作为 systemd 服务运行。${NC}"
echo -e "📖 配置文件: /etc/gnet-proxy/config.jsonc"
echo -e "📊 查看状态: systemctl status gnet-proxy"
echo -e "📜 查看日志: journalctl -u gnet-proxy -f"
echo -e "${GREEN}=====================================================${NC}"
