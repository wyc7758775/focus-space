# Focus Space（焦点空间）

单二进制 Go 应用：RSS / 微信公众号 / B 站订阅 + 会议纪要 + Obsidian 笔记 + AI 概览的阅读与焦点工作台。
独立开发、单仓库自包含;亦作为 NAS 智能助手的应用模板(`plaud-action-dashboard`)分发。

> **命名说明**：项目名是 `focus-space`；设备上的应用 slug 仍是 `plaud-action-dashboard`
> （`metadata.json` 的 `name`、`app_contract.go` 的数据指纹种子、板上 device-data URL、前端
> `scheduleBridgeSlug` 都绑定这个 slug，**改它会破坏已有数据的连续性**）。两者刻意分离，slug
> 只在切换发布源时才考虑统一。

## 架构

```
浏览器
  → apphost 反代 /api/v1/apps/plaud-action-dashboard/http/*
  → 本应用（Go 单二进制，embed 前端 + SQLite，只监听 loopback）
      ├─ device-data 桥（只读）→ local-server 19090：会议 / Obsidian / 会议媒体
      ├─ source-gateway       → local-server 19090：RSS / 微信 / B 站采集（熔断在这层）
      └─ AI 面 / ai-proxy     → local-server 19090 / gateway 9090：文章 AI 概览
```

- 前端：`static/`，无构建工具，由 Go embed 打进二进制（本地开发可切磁盘直读，见下）
- 数据：设备 SQLite `data/app.db`（相对 CWD），迁移在 `migrations/`
- 能力声明：`metadata.json`（device_data / app_data 操作白名单）

## 本地开发（改完刷新即见，无需部署板子）

依赖：本机 Go ≥1.25、可 SSH 到板子。

```bash
# 1. 建配置（dev.yaml 已 gitignore，板子 IP 不入库；也可用 HOST=root@<板子IP> 临时覆盖）
cp dev.example.yaml dev.yaml   # 填 board_host

# 2. 抄板上运行中的应用环境（token + base URL + owner/profile），写入 .dev-env（gitignored）
./scripts/dev/env-from-board.sh

# 3. 建隧道 + 本地起服务
./scripts/dev/up.sh
#    ▶ http://127.0.0.1:18101
```

原理：SSH 同端口转发板上 `local-server:19090` 与 `gateway:9090` 到本机 loopback，
板上抄来的 env URL 原样可用；`ZAPP_STATIC_DIR` 让静态资源从磁盘直读，
改 `static/` 下文件浏览器刷新即生效，改 `*.go` 重跑 `up.sh`（几秒）。

注意事项：

- `.dev-env` 含设备 token，永不提交；板子重置后重跑 `env-from-board.sh`
- 本地写操作（已读 / 收藏 / 布局）落在本地 `.dev-data/`，与板子数据隔离
- **例外：订阅 / 退订会通过真实 token 写穿到板上 source-gateway**，开发时留意
- AI / 会议 / Obsidian / 订阅源内容均为板子真实数据

## 测试

```bash
go test .            # 全部（CI 用）
go test -run TestXxx .   # 定向
```

设备端正式部署走 模板 → 预编译产物 → 应用宿主(apphost)发布链路:
`scripts/release/sync-template.sh` 把白名单物料同步到模板检出目录(`PRESETS_DIR`,默认
`~/zettlab-presets`,可 export 覆盖),`scripts/release/build-prebuilt.sh` 交叉编译并生成
artifact 对账清单。本仓库是开发主现场。

## License

[Apache-2.0](./LICENSE)
