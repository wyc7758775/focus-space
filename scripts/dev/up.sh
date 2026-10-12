#!/usr/bin/env bash
# 本地开发主循环：
#   1. SSH 同端口转发板上的 local-server(19090, device-data/source-gateway/AI 面) 和 gateway(9090, ai-proxy 兜底)
#   2. 用板上抄来的 env(.dev-env) 启动本地构建的应用，静态资源直读本仓库磁盘
# 浏览器打开 http://127.0.0.1:18101 —— 改 static/ 下文件刷新即生效，改 *.go 后重跑本脚本。
# 用法: ./scripts/dev/up.sh   （板子地址/端口读 dev.yaml，可用 HOST=root@<板子IP> 临时覆盖）
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=/dev/null
. "$REPO_ROOT/scripts/dev/_config.sh"

cd "$REPO_ROOT"
[ -f .dev-env ] || { echo "先运行: ./scripts/dev/env-from-board.sh" >&2; exit 1; }
mkdir -p .dev-data

echo "建立隧道: $DEV_BOARD_HOST → 本地 ${DEV_TUNNEL_LOCAL_SERVER}(local-server) / ${DEV_TUNNEL_GATEWAY}(gateway ai-proxy)"
ssh -N \
  -L "${DEV_TUNNEL_LOCAL_SERVER}:127.0.0.1:${DEV_TUNNEL_LOCAL_SERVER}" \
  -L "${DEV_TUNNEL_GATEWAY}:127.0.0.1:${DEV_TUNNEL_GATEWAY}" \
  "$DEV_BOARD_HOST" &
TUNNEL_PID=$!

# 板上应用被停靠（idle 2h / 内存压力 / boot 闸门）时，internal source-gateway /
# device-data 面会在授权层直接 404（设计如此：停靠应用的桥必须断）。本地 dev
# 进程持有抄来的 token 常驻轮询，正好踩这个坑。每 60s 经隧道点一次应用的公共
# 面（< 90s activeUserWindow）——既按需唤醒，又让 governor 视为持续 human touch
# 不停靠，LastOpen 持久化后 boot 拉起也认它。app slug 从 .dev-env 的 BASE_URL 提取。
APP_SLUG=$(sed -n 's|^ZAPP_SOURCE_GATEWAY_BASE_URL=.*/app-runtime/\([^/]*\)/.*$|\1|p' ./.dev-env | head -1)
KEEPALIVE_PID=""
if [ -n "$APP_SLUG" ]; then
  (
    sleep 5  # 等隧道就绪
    while :; do
      curl -sf -m 10 -o /dev/null \
        "http://127.0.0.1:${DEV_TUNNEL_LOCAL_SERVER}/api/v1/apps/${APP_SLUG}/http/" || true
      sleep 60
    done
  ) &
  KEEPALIVE_PID=$!
fi
trap 'kill "$TUNNEL_PID" ${KEEPALIVE_PID:+"$KEEPALIVE_PID"} 2>/dev/null || true' EXIT

echo "构建本机二进制..."
( cd "$REPO_ROOT" && go build -o .dev-data/plaud-dev . )

# .dev-env 里抄来的 base URL 全部指向 127.0.0.1:19090，同端口转发后原样可用；
# device-data 桥的 loopback 强制校验不受影响。
set -a
# shellcheck disable=SC1091
. ./.dev-env
set +a

export ZAPP_LISTEN_ADDR="$DEV_LISTEN_ADDR"
export ZAPP_STATIC_DIR="$REPO_ROOT"

echo
echo "  ▶ http://$DEV_LISTEN_ADDR   (Ctrl-C 退出并关闭隧道)"
echo "  ⚠ 订阅/退订操作会通过真实 token 写穿到板上 source-gateway，开发时注意"
echo
cd .dev-data
exec ./plaud-dev
