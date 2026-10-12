#!/usr/bin/env bash
# 白名单物料同步: focus-space → zettlab-presets 模板目录。
# 模板目录只当「发布物料」，不当开发工作区：先清空目标，再按白名单复制。
# 白名单外的任何文件（*_test.go / *.test.cjs / dev.yaml / .dev-env / scripts/dev|release）都不会过去。
#
# 白名单:
#   *.go（排除 *_test.go）  internal/**（排除 *_test.go）  static/**（排除 *.test.cjs）
#   migrations/**  blueprints/**  catalog/**  scripts/top_rss_catalog.py
#   go.mod  go.sum  metadata.json
#
# 物料预算与 build_runner / 打包器一致: ≤192 文件、≤16MB 总量、单文件 ≤1MB。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PRESETS_DIR="${PRESETS_DIR:-$HOME/zettlab-presets}"  # presets 检出目录;export PRESETS_DIR 覆盖
TARGET="$PRESETS_DIR/templates/plaud-general-agent/skills/plaud-application-create/templates/plaud-action-dashboard"

MAX_FILES=192
MAX_BYTES=$((16 << 20))
MAX_FILE_BYTES=$((1 << 20))

if [ ! -d "$TARGET" ]; then
  echo "目标不存在: $TARGET" >&2
  echo "检查 PRESETS_DIR（当前=${PRESETS_DIR}）" >&2
  exit 1
fi

# 先在暂存目录组装白名单物料，全部通过预算校验后原子替换目标
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cd "$REPO_ROOT"
mkdir -p "$STAGE/static" "$STAGE/migrations" "$STAGE/blueprints" "$STAGE/catalog" "$STAGE/scripts"

find . -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec cp {} "$STAGE/" \;
# 本仓 module 名是 focus-space；模板物料保持设备侧约定 module plaud-action-dashboard
# （artifact.json 的源树哈希也以此为准），internal/ 包的导入路径同步改写
cp go.sum metadata.json "$STAGE/"
sed 's/^module .*/module plaud-action-dashboard/' go.mod > "$STAGE/go.mod"
if [ -d internal ]; then
  rsync -a --exclude '*_test.go' --exclude '.DS_Store' internal/ "$STAGE/internal/"
  find "$STAGE" -name '*.go' -type f -exec sed -i '' 's#"focus-space/internal/#"plaud-action-dashboard/internal/#g' {} +
fi
cp scripts/top_rss_catalog.py "$STAGE/scripts/"
rsync -a --exclude '*.test.cjs' --exclude '.DS_Store' static/ "$STAGE/static/"
rsync -a --exclude '.DS_Store' migrations/ "$STAGE/migrations/"
rsync -a --exclude '.DS_Store' blueprints/ "$STAGE/blueprints/"
rsync -a --exclude '.DS_Store' catalog/ "$STAGE/catalog/"

# 预算校验（在暂存目录上做，不碰目标）
COUNT=$(find "$STAGE" -type f | wc -l | tr -d ' ')
BYTES=$(find "$STAGE" -type f -exec stat -f%z {} + | awk '{s+=$1} END{print s+0}')
echo "物料: ${COUNT} 文件 / ${BYTES} 字节"
if [ "$COUNT" -gt "$MAX_FILES" ] || [ "$BYTES" -gt "$MAX_BYTES" ]; then
  echo "超出模板物料预算（≤${MAX_FILES} 文件 / ≤${MAX_BYTES} 字节），中止" >&2
  exit 1
fi
OVERSIZED=$(find "$STAGE" -type f -size +${MAX_FILE_BYTES}c)
if [ -n "$OVERSIZED" ]; then
  echo "单文件超 1MB 上限:" >&2
  echo "$OVERSIZED" >&2
  exit 1
fi

# 开发文件残留自查（白名单本身应杜绝；此处兜底）
LEAK=$(find "$STAGE" \( -name '*_test.go' -o -name '*.test.cjs' -o -name '.dev-env' -o -name 'dev.yaml' \) -print)
if [ -n "$LEAK" ]; then
  echo "暂存物料混入开发文件（不应发生）:" >&2
  echo "$LEAK" >&2
  exit 1
fi
# 本仓名残留自查（module 行与导入路径必须已改写为模板名；API 路由 /api/focus-space 等业务字符串不算）
if grep -rn '"focus-space/' "$STAGE" --include='*.go' -q || grep -q '^module focus-space' "$STAGE/go.mod"; then
  echo "暂存物料残留 focus-space module/导入路径（改写遗漏）:" >&2
  grep -rn '"focus-space/' "$STAGE" --include='*.go' >&2
  grep -n 'focus-space' "$STAGE/go.mod" >&2
  exit 1
fi

# 原子替换：旧目录移走 → 新目录就位 → 删旧
BACKUP="$TARGET.sync-backup"
rm -rf "$BACKUP"
mv "$TARGET" "$BACKUP"
cp -R "$STAGE" "$TARGET"
rm -rf "$BACKUP"

echo "已同步 → $TARGET"
echo "后续: git -C '$PRESETS_DIR' status 查看 diff，再跑 scripts/release/build-prebuilt.sh"
