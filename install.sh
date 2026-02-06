#!/bin/bash
set -e

# FusionAPI 一键安装脚本
# 用法: curl -fsSL https://raw.githubusercontent.com/YOUR_USER/fusionapi/main/install.sh | bash

REPO="YOUR_USER/fusionapi"
INSTALL_DIR="/usr/local/bin"
DATA_DIR="$HOME/.fusionapi"

# 检测系统和架构
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       echo "不支持的架构: $ARCH"; exit 1 ;;
esac

SUFFIX="${OS}-${ARCH}"
echo "检测到系统: ${OS}/${ARCH}"

# 获取最新版本
echo "获取最新版本..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then
  echo "获取版本失败，请检查网络连接"
  exit 1
fi

echo "最新版本: ${LATEST}"

# 下载
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/fusionapi-${SUFFIX}.tar.gz"
echo "下载 ${DOWNLOAD_URL}..."

TMP_DIR=$(mktemp -d)
curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/fusionapi.tar.gz"

# 解压
echo "安装中..."
mkdir -p "$DATA_DIR"
cd "$TMP_DIR"
tar xzf fusionapi.tar.gz

# 安装二进制
if [ -w "$INSTALL_DIR" ]; then
  cp "fusionapi-${SUFFIX}" "${INSTALL_DIR}/fusionapi"
  chmod +x "${INSTALL_DIR}/fusionapi"
else
  sudo cp "fusionapi-${SUFFIX}" "${INSTALL_DIR}/fusionapi"
  sudo chmod +x "${INSTALL_DIR}/fusionapi"
fi

# 安装配置和前端
if [ ! -f "$DATA_DIR/config.yaml" ]; then
  cp config.yaml "$DATA_DIR/config.yaml"
fi

if [ -d "dist" ]; then
  cp -r dist "$DATA_DIR/"
fi

# 清理
rm -rf "$TMP_DIR"

echo ""
echo "========================================="
echo "  FusionAPI ${LATEST} 安装成功！"
echo "========================================="
echo ""
echo "  配置文件: $DATA_DIR/config.yaml"
echo "  启动命令: cd $DATA_DIR && fusionapi"
echo "  访问地址: http://localhost:8080"
echo ""
