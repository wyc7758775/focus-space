#!/usr/bin/env bash
# 从板上运行中的焦点空间应用（slug: plaud-action-dashboard）进程抄全套 ZAPP_* 环境变量 → .dev-env
# 含 device-data / source-gateway / AI 面的 base URL 和 token、owner/profile 过滤 ID。
# 用法: ./scripts/dev/env-from-board.sh   （板子地址读 dev.yaml，可用 HOST=root@<板子IP> 临时覆盖）
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=/dev/null
. "$REPO_ROOT/scripts/dev/_config.sh"

cd "$REPO_ROOT"

# 板上进程名不保证等于应用 slug，直接在 /proc 里找被注入了 ZAPP_DEVICE_DATA_TOKEN 的进程。
ENV_CONTENT=$(ssh "$DEV_BOARD_HOST" '
  for p in /proc/[0-9]*/environ; do
    if tr "\0" "\n" < "$p" 2>/dev/null | grep -q "^ZAPP_DEVICE_DATA_TOKEN="; then
      tr "\0" "\n" < "$p"
      exit 0
    fi
  done
  exit 1
') || { echo "未在板上找到带 ZAPP_DEVICE_DATA_TOKEN 的进程；确认应用在运行、SSH 可达" >&2; exit 1; }

# 只保留 ZAPP_*（排除板上的 PATH/HOME），并去掉本地面要覆盖的监听地址。
printf '%s\n' "$ENV_CONTENT" | grep '^ZAPP_' | grep -v '^ZAPP_LISTEN_ADDR=' > .dev-env
chmod 600 .dev-env

COUNT=$(wc -l < .dev-env | tr -d ' ')
echo "已写入 .dev-env（${COUNT} 个 ZAPP_* 变量）。内含设备 token："
echo "  - 已在 .gitignore，永不提交"
echo "  - 板子重置/重新配网后 token 会轮换，重跑本脚本即可"
