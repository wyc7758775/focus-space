# Focus Space（焦点空间）

独立的 Go 应用：RSS / 微信公众号 / B 站订阅、会议、Obsidian、阅读与焦点工作台。
以 `.zapp` 应用包导入 **Zettlab Memo**，从手机和桌面的应用列表打开。

内部应用名固定为 `plaud-action-dashboard`，显示名为“焦点空间”。旧数据指纹、设备桥和焦点回合均依赖这个名字；导入不得自动加后缀。同名升级须显式选择 `--replace`，保留私有 `data/` 和已有版本。

## 生成应用包

GitHub Actions 的 **Focus Space application package** 工作流在推送 `codex/focus-space-app` 分支或 `v*` 标签时运行，生成 `linux-arm64`（开发板）与 `linux-amd64` 两份 artifact。工作流进入默认分支后也支持手动运行。下载并解压 artifact，得到 `.zapp` 文件。源码和二进制必须来自同一次 CI 构建。

CLI 使用 Python 3.11+，只依赖标准库，支持 Windows / macOS / Linux。CI 内部执行：

```sh
python scripts/zapp.py stage --output dist/source
# 在 CI 的 dist/source 内编译 ./src，得到 dist/app
python scripts/zapp.py pack --source dist/source --binary dist/app --revision <git-commit> --output dist/focus-space-linux-arm64.zapp
python scripts/zapp.py inspect dist/focus-space-linux-arm64.zapp
```

`stage` 把根目录 Go 和 embed 资源转换成平台维护使用的 `src/` 布局，排除开发入口与测试；`pack` 使用 App Host v1 manifest 和逐文件 SHA-256。只收录生产文件，排除数据库、日志、开发配置、token、安装归属和已授权能力。当前包未签名，哈希只校验完整性，不证明发布者身份。产物总上传体积须不超过现有网关的 **32 MiB**，CI 会检查。

仓库原来的 `scripts/release/sync-template.sh` 和 `build-prebuilt.sh` 已退役，不能再向旧 Agent 模板发布。本工作区禁止本地全量 build / QA，二进制编译由 CI 完成。

## 用 CLI 导入

设备先升级到支持 `preserve_slug_v1` / `explicit_device_data_v1` 的 App Host。CLI 会先探测能力，旧设备在上传前报错。设备地址使用能从本机访问的 **Gateway** 地址（通常 `9090`）；云端 API token 不能替代设备 JWT。

```sh
python scripts/zapp.py import dist/focus-space-linux-arm64.zapp --device http://<设备地址>:9090 --grant device_meetings_read --grant obsidian_read
```

在交互终端按提示输入当前用户的设备 JWT，输入不回显，也不放进命令参数或仓库。此命令明确同意读取会议与 Obsidian。CLI 会校验包、流式上传、核对返回的应用身份与权限回执。

按实际使用场景增加以下授权，不传 `--grant` 就不新增设备数据权限：

| 参数 | 能力 |
|---|---|
| `--grant device_meetings_read` | 读取会议资料 |
| `--grant obsidian_read` | 读取 Obsidian 笔记 |
| `--grant source_gateway_read` | 使用订阅源网关；包括订阅管理，会写入设备订阅状态 |
| `--grant focus_decision_turn` | 经平台运行焦点决策回合 |
| `--grant focus_chat_media_read` | 读取焦点聊天授权的会议媒体 |

AI 按 `spec.yaml` 的 `ai: {}` 走平台既有授权流程，不使用云 token。

升级同名应用，在原命令后加 `--replace`。**只有原安装用户才能在升级时批准设备数据权限**；旧应用缺少可验证创建者时拒绝新增授权，需要单独迁移身份，不能冒认归属。省略 `--replace` 遇到重名会停止。更新不会撤掉旧权限，也不会删除用户数据；撤权需走平台管理流程。

HTTP 错误、跳转和不完整回执不会自动重试。若上传后断线，先在 Memo 应用列表检查安装结果，再决定是否执行显式升级，避免重复操作。

## 运行方式与当前范围

```text
Memo 手机 / 桌面应用列表
  → Gateway → App Host /api/v1/apps/plaud-action-dashboard/http/*
  → Focus Space（宿主分配的 loopback 地址）
      ├─ embed 前端、迁移；私有 data/app.db
      ├─ 按授权注入的 device-data / source-gateway / focus-decision 桥
      └─ ZAPP_AI_BASE_URL + ZAPP_AI_TOKEN → 应用专属 AI /status 与 /complete
```

AI 请求有输入、输出和响应体上限及 35 秒总超时；额度不足、忙、超时、异常结构结束本次请求，由用户手动重试。成功摘要按既有队列写入 SQLite，再次打开复用结果。缺少能力时保留缓存阅读；不回退到通用 AI 代理绕过授权。

本次实现独立打包、显式授权导入、生产静态资源隔离和文章 AI 接入。旧日程消息、应用内后台轮询、完整 Vue / SSE / 九语适配、平台离线重编译、设备 RSS 测量和手机/桌面 GUI 验收仍待后续验证，不能以“导入成功”代替这些验收。

## 本地开发

依赖 Go ≥1.25；仅开发构建使用 `-tags dev`，允许 `ZAPP_STATIC_DIR` 读取磁盘。正式构建始终使用 embed，忽略该变量。

已有 SSH 开发脚本保留：复制 `dev.example.yaml` 为 gitignored 的 `dev.yaml`，配置目标设备，再运行 `scripts/dev/env-from-board.sh` 与 `scripts/dev/up.sh`。`.dev-env` 中的设备凭证不得提交；本地数据在 `.dev-data/`，但订阅操作和 AI 会调用真实设备服务。不要将开发配置打包分发。

## 定向验证

```sh
go test . ./internal/bridges -run 'TestPlatformAI|TestProductionStaticAssets|TestDeviceDataBridge' -count=1
go test -tags dev . -run 'TestStaticAssetsFS|TestValidateStaticDirOverride' -count=1
python -m unittest discover -s scripts -p test_zapp.py
node --test --test-name-pattern='platform AI failure flow' reader_rendering_flow.test.cjs
```

CLI 测试用 ELF 头作为包格式 fixture，不可运行，不能当发布产物。真实 `.zapp` 必须经过 CI 编译及目标设备验证。

## License

[Apache-2.0](./LICENSE)
