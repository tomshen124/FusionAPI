#!/usr/bin/env bash
set -euo pipefail

# FusionAPI one-command installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/tomshen124/FusionAPI/main/install.sh | bash

REPO="tomshen124/FusionAPI"
BIN_NAME="fusionapi"
DEFAULT_INSTALL_DIR="/usr/local/bin"
FALLBACK_INSTALL_DIR="$HOME/.local/bin"
DATA_DIR="$HOME/.fusionapi"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH_RAW="$(uname -m)"

case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH_RAW"
    exit 1
    ;;
esac

case "$OS" in
  linux|darwin) ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

SUFFIX="${OS}-${ARCH}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

INSTALL_DIR="$DEFAULT_INSTALL_DIR"
if [ ! -w "$INSTALL_DIR" ] && ! command -v sudo >/dev/null 2>&1; then
  INSTALL_DIR="$FALLBACK_INSTALL_DIR"
fi

echo "Detected platform: ${OS}/${ARCH}"
echo "Install dir: ${INSTALL_DIR}"

copy_bin() {
  local src="$1"
  mkdir -p "$INSTALL_DIR"
  if [ -w "$INSTALL_DIR" ]; then
    cp "$src" "${INSTALL_DIR}/${BIN_NAME}"
    chmod +x "${INSTALL_DIR}/${BIN_NAME}"
  else
    sudo mkdir -p "$INSTALL_DIR"
    sudo cp "$src" "${INSTALL_DIR}/${BIN_NAME}"
    sudo chmod +x "${INSTALL_DIR}/${BIN_NAME}"
  fi
}

install_assets() {
  local src_root="$1"
  mkdir -p "$DATA_DIR"
  if [ ! -f "${DATA_DIR}/config.yaml" ] && [ -f "${src_root}/config.yaml" ]; then
    cp "${src_root}/config.yaml" "${DATA_DIR}/config.yaml"
  fi
  if [ -d "${src_root}/dist" ]; then
    rm -rf "${DATA_DIR}/dist"
    cp -r "${src_root}/dist" "${DATA_DIR}/"
  fi
}

install_from_release() {
  local latest
  latest="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n1)"

  if [ -z "$latest" ]; then
    return 1
  fi

  local archive_url="https://github.com/${REPO}/releases/download/${latest}/fusionapi-${SUFFIX}.tar.gz"
  local archive_path="${TMP_DIR}/fusionapi.tar.gz"
  echo "Trying release install: ${latest}"
  curl -fsSL "$archive_url" -o "$archive_path" || return 1

  local release_dir="${TMP_DIR}/release"
  mkdir -p "$release_dir"
  tar xzf "$archive_path" -C "$release_dir"

  local binary_path=""
  if [ -f "${release_dir}/fusionapi-${SUFFIX}" ]; then
    binary_path="${release_dir}/fusionapi-${SUFFIX}"
  elif [ -f "${release_dir}/${BIN_NAME}" ]; then
    binary_path="${release_dir}/${BIN_NAME}"
  fi
  if [ -z "$binary_path" ]; then
    return 1
  fi

  copy_bin "$binary_path"
  install_assets "$release_dir"
  echo "Installed from release: ${latest}"
  return 0
}

install_from_source() {
  echo "Release artifact not available, falling back to source build..."
  command -v git >/dev/null 2>&1 || { echo "git is required for source fallback"; exit 1; }
  command -v go >/dev/null 2>&1 || { echo "go is required for source fallback"; exit 1; }

  local src_dir="${TMP_DIR}/src"
  git clone --depth=1 "https://github.com/${REPO}.git" "$src_dir"

  if command -v npm >/dev/null 2>&1; then
    (cd "${src_dir}/web" && npm install && npm run build)
  else
    echo "npm not found, skipping Web UI build in source fallback"
  fi

  (cd "$src_dir" && go build -o "${BIN_NAME}" ./cmd/fusionapi)
  copy_bin "${src_dir}/${BIN_NAME}"
  install_assets "$src_dir"
}

if ! install_from_release; then
  install_from_source
fi

# Setup systemd service on Linux (if running as root or with sudo)
setup_systemd() {
  if [ "$OS" != "linux" ]; then return; fi
  if ! command -v systemctl >/dev/null 2>&1; then return; fi

  local service_file="/etc/systemd/system/fusionapi.service"
  local can_write=false

  if [ -w "/etc/systemd/system" ]; then
    can_write=true
  elif command -v sudo >/dev/null 2>&1; then
    can_write=true
  fi

  if [ "$can_write" = false ]; then return; fi

  local svc_content="[Unit]
Description=FusionAPI - AI API Aggregation Gateway
After=network.target

[Service]
Type=simple
WorkingDirectory=${DATA_DIR}
ExecStart=${INSTALL_DIR}/${BIN_NAME} -config ${DATA_DIR}/config.yaml
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target"

  if [ -w "/etc/systemd/system" ]; then
    echo "$svc_content" > "$service_file"
    systemctl daemon-reload
    systemctl enable fusionapi
    systemctl restart fusionapi
  else
    echo "$svc_content" | sudo tee "$service_file" >/dev/null
    sudo systemctl daemon-reload
    sudo systemctl enable fusionapi
    sudo systemctl restart fusionapi
  fi

  echo "Systemd service installed and started."
}

setup_systemd

echo ""
echo "========================================="
echo "FusionAPI installed successfully"
echo "========================================="
echo "Binary: ${INSTALL_DIR}/${BIN_NAME}"
echo "Config: ${DATA_DIR}/config.yaml"
if [ "$OS" = "linux" ] && command -v systemctl >/dev/null 2>&1; then
  echo ""
  echo "Service commands:"
  echo "  systemctl status  fusionapi"
  echo "  systemctl stop    fusionapi"
  echo "  systemctl restart fusionapi"
  echo "  journalctl -u fusionapi -f    # view logs"
else
  echo "Run:    ${BIN_NAME} -config ${DATA_DIR}/config.yaml"
fi
echo ""
echo "Web UI: http://<your-ip>:18080"
if [ "$INSTALL_DIR" = "$FALLBACK_INSTALL_DIR" ]; then
  echo ""
  echo "Add to PATH if needed:"
  echo "  export PATH=\"${FALLBACK_INSTALL_DIR}:\$PATH\""
fi
