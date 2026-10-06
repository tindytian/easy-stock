# 个人版维护说明:Tushare 中转支持(codex/tushare-relay)

本文档记录个人 Fork 的上游基准、改动范围、验证结果、打包与安装回退方法,以及以后合并官方新版的固定流程。任何人(包括未来的自己或 AI 助手)按此文档即可继续维护。

## 1. 仓库与远端约定

| 项 | 值 |
| --- | --- |
| 官方上游 | https://github.com/jundizhou/easy-stock (`upstream`,只读,永不推送) |
| 个人 Fork | https://github.com/tindytian/easy-stock (`origin`,唯一推送目标) |
| 维护分支 | `codex/tushare-relay` |
| 上游基准 | 官方 tag `v1.4.0` = commit `a24749c1fc317dcb7fba7304b6015ee87191173f`(2026-10-04) |

每次推送前核对:`git remote -v` 确认 `origin` 指向个人 Fork;只执行 `git push origin codex/tushare-relay`,不使用 `--force`。

## 2. 改动范围(相对官方 v1.4.0)

功能:系统设置新增「Tushare 接口地址」与「测试连接」。默认留空使用官方地址 `http://api.tushare.pro`;用户可填写兼容 Tushare 报文的中转 URL(支持 HTTP/HTTPS,保留路径与端口)。测试连接会先保存设置,再用已保存的 Token 向该地址发起一次标准 Tushare 请求(`api_name`/`token`/`params`/`fields`,探针接口 `trade_cal`,一日数据,10 秒超时)。

- `backend/internal/appsettings/store.go` — `Values` 新增 `tushare_api_url` 字段与 `DefaultTushareAPIURL` 常量;沿用原有 0600 权限原子写入的 settings.json。
- `backend/internal/httpapi/settings.go` — 设置 GET 视图/PUT 请求新增该字段;校验必须为 http/https URL(≤512 字符),传空串即恢复默认。
- `backend/internal/httpapi/tushare_connection.go`(新增)— `POST /api/v1/settings/tushare/test` 探针:读已保存配置,发标准报文,透出中转 `code/msg` 错误;所有对外错误消息显式剔除原始 Token(叠加在框架 `runtimelog.Redact` 之上)。
- `backend/internal/httpapi/server.go` — 注册上述路由。
- `frontend/src/lib/backend.ts` — `AppSettings` 新增 `tushare_api_url`,新增 `TushareConnectionTestResult` 类型。
- `frontend/src/components/SettingsDrawer.tsx` — 「行情与内容数据源」分区新增地址输入框 + 测试按钮(本地显示结果),保存时一并提交;Token 输入框提示更新。
- `frontend/src/styles.css` — 新增输入框/按钮/结果提示的最小样式。

明确不变的部分:

- **没有把任何行情数据源切换为 Tushare。** 官方 v1.4.0 中 `tushare_token` 只是保存的凭据(界面标注"预留"),代码中不存在任何 Tushare 实际调用;本改动只增加了"测试连接"这一条真实请求链路,不改变 sina/tencent/eastmoney 等公共行情来源。
- Token 存储沿用原有 `credentials.tushare_token`(settings.json,0600);GET 只返回 `configured`+掩码;清空沿用 `clear_secrets`。
- 用户的默认中转地址(如 `http://111.170.140.159:8020/`)是**用户配置**,未写入源码;官方地址是唯一内置默认值。

## 3. 用户配置方法

1. 打开 easy-stock → 设置 → 「行情与内容数据源」。
2. 「Tushare Pro Token」填入自己的 Token(点保存后写入本机 settings.json)。
3. 「Tushare 接口地址」填中转地址(例如 `http://<host>:<port>/`,可带路径),留空则用官方地址。
4. 点「测试连接」:按钮会先保存设置再发起探针;成功显示 `trade_cal 返回 N 行数据`,失败显示中转返回的错误码/消息或网络错误提示。

## 4. 构建、测试与打包

环境要求:macOS、Node ≥ 24、Go ≥ 1.26、`uv`(Hermes 运行时)、Homebrew。本机已配置 `go env -w GOPROXY=https://goproxy.cn,direct`(proxy.golang.org 不可达)。

```bash
npm test              # 官方入口:后端 go test ./... + 前端 vitest
npm run installer:mac # 完整打包:前端+后端+Codex/Hermes/微信运行时 → DMG
npm run package:mac   # 仅打目录包(不产 DMG),速度更快
```

后端单测(含新增 Tushare 用例):`cd backend && go test ./internal/httpapi/ ./internal/appsettings/`。

端到端验证脚本(本地模拟中转,不碰真实用户数据,升级后直接复跑):

```bash
bash scripts/tushare-relay-e2e/run-e2e.sh   # 可选参数:后端端口,默认 20099
```

脚本把所有数据路径指向 `/tmp/easy-stock-e2e/data`,验证:保存回显、重启恢复、自定义地址生效、请求格式(`api_name/token/params/fields`)、无 Token 412、中转业务错误透出、非法协议 400、settings.json 0600、日志无明文 Token。本轮结果 **18/18 通过**。

## 5. 验证结果记录(2026-10-06)

| 检查项 | 结果 |
| --- | --- |
| 后端全量 `go test ./...` | ✅ 全部通过(官方基线中 `providers/eastmoney TestProgressiveHistoryPublishesRecentDayBeforeSlowHistory` 偶发 `unexpected parallelism 1`,时序敏感,与本改动无关) |
| 前端 `vitest --run` | ✅ 27 文件 168 用例全过 |
| 前端 `tsc -b && vite build` | ✅ 通过 |
| 新增 Tushare 单测 | ✅ 8 个用例全过 |
| E2E(模拟中转) | ✅ 18/18 |
| `desktop` 层 `node --test` | ⚠️ 64 用例中 1 个既有失败:`release-publish.test.cjs` 的 OSS 上传测试(官方发布脚本,本机环境不兼容;本改动未触碰任何 desktop 文件)→ 该项 **NOT VERIFIED(本机)**,官方 CI 应为通过;打包入口 `installer:mac` 不依赖它 |
| 真实中转(用户 Token) | ⏳ **NOT VERIFIED——需用户在应用内填真实 Token 后点「测试连接」**(见第 8 节步骤) |
| 桌面 GUI 操作 | ⏳ 建议用户按第 8 节在 DMG 安装后确认界面与按钮;本机已用后端 API 级 E2E 覆盖同链路 |

## 6. 安装、用户数据与回退

- 用户数据目录:`~/Library/Application Support/easy-stock`(settings.json、各 *.db、logs、hermes-home 等)。**覆盖安装只替换 /Applications/easy-stock.app,不清除该目录**(官方设计,已在 `desktop/data-protection.cjs` 核实备份/排除逻辑)。
- 覆盖安装前手动备份(可选但推荐):

```bash
tar -czf "$HOME/Desktop/easy-stock-data-$(date +%Y%m%d-%H%M).tgz" \
  -C "$HOME/Library/Application Support" easy-stock \
  --exclude "easy-stock/Cache" --exclude "easy-stock/Code Cache" --exclude "easy-stock/GPUCache"
```

- 回退:重新安装此前保留的官方 v1.4.0 DMG(或官方 Releases 下载),覆盖即可;数据目录向后兼容,`tushare_api_url` 字段对官方版本是未知字段,会被忽略,不影响使用。
- 应用内更新备份目录(Windows 自动更新用):`~/Library/Application Support/easy-stock-update-backups`。

## 7. 自动更新行为核实(无需改动)

- `desktop/update-manager.cjs:36-37`:`autoDownload = false`、`autoInstallOnAppQuit = false` —— **任何平台都不会未经确认下载或安装**。
- macOS 上 `canInstallAutomatically=false` → `installMode='manual'`,更新面板只显示「前往下载新版」,链接指向官方 GitHub Releases(手动下载覆盖安装)。
- 更新源为官方 OSS(`https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop`),个人版无签名,electron-updater 校验不过也不会自动替换;启动+每 12 小时一次的检查只是提示。
- 结论:个人版不会被官方安装包自动覆盖,同时保留了"有官方新版"的提示,符合要求,未做任何改动。

## 8. 真实中转验证步骤(用户操作)

1. 安装本仓库打包的 DMG,启动 easy-stock。
2. 设置 → 「行情与内容数据源」:「Tushare 接口地址」填 `http://111.170.140.159:8020/`(以实际中转为准),Token 填真实值,保存。
3. 点「测试连接」。预期:几秒内显示「连接成功,trade_cal 返回 1 行数据」;若中转要求特定积分/权限,错误消息会透出 Tushare 的 code/msg。
4. 重启 App 再点一次「测试连接」,确认配置持久化。
5. (可选)抓包确认请求为标准报文:`{"api_name":"trade_cal","token":"…","params":{"exchange":"SSE","start_date":"…","end_date":"…"},"fields":"cal_date,is_open"}`。

## 9. 以后升级到官方新版的固定流程

```bash
cd <仓库> && git status --porcelain          # 必须干净
git fetch upstream --tags                    # 获取官方 tags
git checkout codex/tushare-relay && git pull origin codex/tushare-relay
git checkout -b upgrade/v<新版本>             # 从当前个人版新建升级分支
git merge v<新版本>                           # 合并官方发行 tag(不是 merge --squash)
# 解决冲突:优先保留官方上游实现,再重新套用本文件第 2 节的改动点
npm test                                     # 官方测试入口
cd backend && go test ./internal/httpapi/ ./internal/appsettings/   # 中转相关用例
bash scripts/tushare-relay-e2e/run-e2e.sh          # E2E 复跑
npm run installer:mac                        # 重新打包
git checkout codex/tushare-relay && git merge upgrade/v<新版本>     # 验证通过后合回
git tag tushare-relay-v<新版本>-1            # 可回退的 Git 标记(个人版命名,避免与官方 tag 冲突)
git push origin codex/tushare-relay --follow-tags   # 只推个人 Fork
```

要点:

1. 先检查工作树干净;选择**明确的官方发行 tag**(不用 main 随机 commit)。
2. 从个人版新建升级分支,把官方 tag **合并**进来;冲突时以官方为准重放第 2 节改动,保持改动最小。
3. 重新验证:中转配置保存/恢复、请求格式、凭据保护(掩码/日志)、`npm test`、E2E、打包。
4. 通过后合回 `codex/tushare-relay`,打个人 tag,推送个人 Fork,交付新 DMG。
5. 保留上一版 DMG 与 Git 标记用于回退;**不自动改写已发布历史,不 force push**。
6. 每次升级后更新本文档的基准 tag/commit 与验证记录表。

## 10. 红线(每次操作前自查)

- 只向 `origin`(个人 Fork)推送;核对 `git remote -v`。
- 不提交:Token、settings.json、任何 *.db、构建产物(desktop/resources、dist、DMG)。
- 不读取 Trading-OS 的真实数据库取 Token;Token 一律由用户在应用界面填写。
- 不强推、不改写远程历史;官方仓库 `upstream` 只拉取。
