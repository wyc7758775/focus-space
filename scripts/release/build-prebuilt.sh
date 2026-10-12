#!/usr/bin/env bash
# ARM64 预编译 + artifact.json：
#   1. 在「同步后的 presets 模板目录」内交叉编译（二进制 ↔ 源树哈希天然一致）
#   2. source_tree_sha256 直接 import 打包器的 _source_tree_sha256（算法单一事实源，不复制）
#   3. 产物写入 skill assets: assets/prebuilt/plaud-action-dashboard/linux-arm64/{app, artifact.json}
#   4. 末尾用同一套算法对账，失败不留半成品
# 前置: 先跑 scripts/release/sync-template.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PRESETS_DIR="${PRESETS_DIR:-$HOME/zettlab-presets}"  # presets 检出目录;export PRESETS_DIR 覆盖
SKILL_DIR="$PRESETS_DIR/templates/plaud-general-agent/skills/plaud-application-create"
TEMPLATE="$SKILL_DIR/templates/plaud-action-dashboard"
ASSET_DIR="$SKILL_DIR/assets/prebuilt/plaud-action-dashboard/linux-arm64"
BUILDER="$PRESETS_DIR/scripts/build-agent-template-package.py"

ARTIFACT_NAME=plaud-action-dashboard
PREBUILT_MAX_BYTES=$((32 << 20))

# 打包器顶层 import jsonschema/yaml：本机缺依赖时用 uv 临时环境跑（结果一致）
if ! python3 -c 'import jsonschema, yaml' >/dev/null 2>&1; then
  PY_RUN=(uv run --quiet --with jsonschema --with pyyaml python3)
else
  PY_RUN=(python3)
fi

if [ ! -f "$TEMPLATE/go.mod" ]; then
  echo "模板目录缺 go.mod —— 先跑: scripts/release/sync-template.sh" >&2
  exit 1
fi
if [ ! -f "$BUILDER" ]; then
  echo "打包器不存在: ${BUILDER}（检查 PRESETS_DIR）" >&2
  exit 1
fi

# 交叉编译（旗标与板上 build_runner / 历史 artifact.json build 块一致）
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "交叉编译 linux/arm64（模板目录: ${TEMPLATE}）..."
( cd "$TEMPLATE" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -mod=mod -p=1 -gcflags all=-l -ldflags "-s -w -buildid=" -trimpath -buildvcs=false \
  -o "$WORK/app" . )

BIN_SIZE=$(stat -f%z "$WORK/app")
BIN_SHA=$(shasum -a 256 "$WORK/app" | awk '{print $1}')
if [ "$BIN_SIZE" -gt "$PREBUILT_MAX_BYTES" ]; then
  echo "二进制 ${BIN_SIZE} 字节超 32MB 上限" >&2
  exit 1
fi
echo "二进制: ${BIN_SIZE} 字节 sha256=${BIN_SHA:0:12}..."

# 源树哈希：import 真实现（__main__ 守卫，无副作用）
SRC_SHA=$("${PY_RUN[@]}" - "$BUILDER" "$TEMPLATE" "$ARTIFACT_NAME" <<'PY'
import importlib.util, pathlib, sys
builder_path, root, name = sys.argv[1], pathlib.Path(sys.argv[2]), sys.argv[3]
spec = importlib.util.spec_from_file_location("zpk_builder", builder_path)
mod = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = mod  # dataclass 装饰器在 exec 期需要 sys.modules 里有本模块
spec.loader.exec_module(mod)
print(mod._source_tree_sha256(root, name))
PY
)
echo "source_tree_sha256=${SRC_SHA:0:12}..."

# 写产物（先全部落到工作目录，对账通过后一次性覆盖 assets）
mkdir -p "$WORK/out"
cat > "$WORK/out/artifact.json" <<EOF
{
  "schema_version": 1,
  "artifact": "$ARTIFACT_NAME",
  "target": "linux/arm64",
  "binary": "app",
  "binary_sha256": "$BIN_SHA",
  "binary_size_bytes": $BIN_SIZE,
  "source_tree_sha256": "$SRC_SHA",
  "build": {
    "goos": "linux",
    "goarch": "arm64",
    "cgo_enabled": 0,
    "go_flags": "-mod=mod -p=1",
    "gcflags": "all=-l",
    "ldflags": "-s -w -buildid=",
    "trimpath": true,
    "buildvcs": false
  }
}
EOF
cp "$WORK/app" "$WORK/out/app"

# 对账：重读产物，用同一套算法验证（与打包器消费端一致）
"${PY_RUN[@]}" - "$BUILDER" "$WORK/out" "$TEMPLATE" <<'PY'
import hashlib, importlib.util, json, pathlib, sys
builder_path, out_dir, template = sys.argv[1], pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3])
spec = importlib.util.spec_from_file_location("zpk_builder", builder_path)
mod = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = mod  # dataclass 装饰器在 exec 期需要 sys.modules 里有本模块
spec.loader.exec_module(mod)
art = json.loads((out_dir / "artifact.json").read_text(encoding="utf-8"))
h = hashlib.sha256()
with open(out_dir / "app", "rb") as f:
    for chunk in iter(lambda: f.read(1 << 20), b""):
        h.update(chunk)
assert art["schema_version"] == 1 and art["target"] == "linux/arm64" and art["binary"] == "app"
assert art["binary_sha256"] == h.hexdigest(), "binary_sha256 对账失败"
assert art["binary_size_bytes"] == (out_dir / "app").stat().st_size, "binary_size_bytes 对账失败"
assert art["source_tree_sha256"] == mod._source_tree_sha256(template, art["artifact"]), "source_tree_sha256 对账失败"
print("artifact 自查通过")
PY

mkdir -p "$ASSET_DIR"
cp "$WORK/out/app" "$ASSET_DIR/app"
cp "$WORK/out/artifact.json" "$ASSET_DIR/artifact.json"
echo "已写入: $ASSET_DIR/{app, artifact.json}"
echo "后续: 提交 presets（模板物料 + 预编译产物），再打 hshare 包"
