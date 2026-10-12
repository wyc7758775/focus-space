#!/usr/bin/env bash
# 读取 dev.yaml（扁平 "key: value" 子集，不引 YAML 依赖）→ 导出 DEV_* 变量。
# 供 env-from-board.sh / up.sh source 使用，不单独执行；HOST 环境变量可覆盖 board_host。

# shellcheck shell=bash
if [ -z "${REPO_ROOT:-}" ]; then
  echo "scripts/dev/_config.sh: 调用方未设置 REPO_ROOT" >&2
  return 1 2>/dev/null || exit 1
fi

DEV_CONFIG="${DEV_CONFIG:-$REPO_ROOT/dev.yaml}"

# 默认值与 dev.example.yaml 保持一致
DEV_BOARD_HOST=""
DEV_LISTEN_ADDR="127.0.0.1:18101"
DEV_TUNNEL_LOCAL_SERVER="19090"
DEV_TUNNEL_GATEWAY="9090"

if [ ! -f "$DEV_CONFIG" ]; then
  echo "缺少 $DEV_CONFIG —— 先执行: cp dev.example.yaml dev.yaml 并填板子 IP" >&2
  return 1 2>/dev/null || exit 1
fi

# 解析扁平 "key: value"；去行内注释（" #" 之后）；未知键忽略，保持向前兼容
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    '#'* | '') continue ;;
    *[![:space:]]*) ;; # 有实际内容，往下解析
    *) continue ;; # 纯空白行
  esac
  key="${line%%:*}"
  value="${line#*:}"
  value="${value%%\ \#*}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  case "$key" in
    board_host) DEV_BOARD_HOST="$value" ;;
    listen_addr) DEV_LISTEN_ADDR="$value" ;;
    tunnel_local_server) DEV_TUNNEL_LOCAL_SERVER="$value" ;;
    tunnel_gateway) DEV_TUNNEL_GATEWAY="$value" ;;
  esac
done < "$DEV_CONFIG"

# HOST 环境变量优先于 dev.yaml（一次性指定板子时用）
if [ -n "${HOST:-}" ]; then
  DEV_BOARD_HOST="$HOST"
fi

if [ -z "$DEV_BOARD_HOST" ]; then
  echo "dev.yaml 缺 board_host（或用 HOST=root@<板子IP> 覆盖）" >&2
  return 1 2>/dev/null || exit 1
fi

case "$DEV_TUNNEL_LOCAL_SERVER$DEV_TUNNEL_GATEWAY" in
  *[!0-9]*)
    echo "dev.yaml 隧道端口必须是数字: tunnel_local_server / tunnel_gateway" >&2
    return 1 2>/dev/null || exit 1
    ;;
esac
