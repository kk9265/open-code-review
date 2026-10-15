# 个人 Code Review 平台实施计划

更新：2026-10-15。基于上游 ea5ced2，外围精简已执行。

## 1. 产品边界

本机单人使用，不建立账号、团队、角色或租户系统。通过浏览器管理审查、持续评论、AI 对话和多轮复审。

代码来源为个人 Gitea 服务 http://10.30.142.20:3000/，账号 kk9265。平台通过 API 获取可访问仓库，通过 HTTPS 或 SSH 拉取至本机管理目录；这不是 SSH 登录服务器。凭据单独配置，不写入代码或历史结果。支持 fetch 后创建新一轮审查，默认不向 Gitea 回写评论或推送代码。

服务仅监听 loopback，远程访问可通过 SSH 隧道。应用依赖本机操作系统账户保护，不隔离本机能够访问端口的其他进程。模型消息角色不是用户体系。运行平台仅支持 Windows 和 Linux，不建设其他平台的构建与发布流程。

## 2. 当前基线

已移除上游介绍、README 翻译、宣传图片、文档网站、ACP、IDE 插件实现、AI 工具插件、npm 分发、安装器、GitHub 流水线和发布脚本。对应的文档/发布契约测试一并移除；Git 行为测试已使用独立样例，不依赖被删除的文档。

保留 Go 审查核心、CLI、只读 viewer、核心测试、内嵌审查规则、LICENSE、AGENTS.md、本开发计划和本机工具链说明。内嵌规则中的 Markdown 是运行时提示词，不是宣传文档。

两份 provider 生成文件暂时保留，供生成器测试和仓库规则使用，extensions 目录已不包含可构建插件。

Go 1.26.9 位于 .local-go/sdk/go，通过 scripts/go.ps1 调用；系统 Go 保持 1.24.3。直接使用 Go 命令构建，不依赖 Make。详见 docs/local-go-windows.md。

新平台入口为 `cmd/ocr-web`，已实现 Gin 服务、SQLite 自动迁移、总览、仓库登记、审查草稿、设置页面，以及编辑、搜索、分页、归档和恢复。页面和 CSS 内嵌到二进制；默认监听 `127.0.0.1:5484`，数据保存在 `.ocr-data/workbench.db`。设置预填个人 Gitea 地址和账号，密钥仅识别进程环境中的配置状态。

Gitea 接入代码已实现：令牌认证、账号校验、仓库搜索与分页、幂等导入、后台拉取、失败重试和中断恢复。代码缓存使用数据目录下的独立 bare 仓库；通过 ID 识别仓库，拉取不会修改用户已有工作区。服务启动使用数据目录锁避免多个同步 worker 操作同一份缓存。当前已确认服务版本为 1.21.4；真实私有仓库联调还需要用户在本机输入令牌。

尚未连接模型或执行审查；草稿中的分支和提交暂存为文本。评论、聊天和复审工作台仍待实现。`opencodereview viewer` 是原有只读 viewer，与新平台入口分开。

## 3. 复用边界

| 位置 | 用途 |
| --- | --- |
| internal/agent、internal/llmloop | 审查编排、预算、工具调用、上下文处理 |
| internal/diff、internal/gitcmd | Git 操作、Diff 解析、代码定位 |
| internal/llm、internal/config | 模型协议、规则与配置 |
| internal/tool、internal/model | 只读工具、评论收集、结果模型 |
| internal/session | 旧历史导入、manifest、恢复和轮次比较 |
| internal/viewer | 只读页面、Diff 和导出参考 |
| cmd/opencodereview | 迁移期 CLI 调试入口 |

Web handler 不直接调用 Cobra 命令。配置组装提取到可复用服务，避免 stdout 切换、信号处理和全局状态进入 HTTP 请求。session.Compare 不替代平台稳定的问题 ID；模型内部工具消息不替代用户讨论线程。

## 4. 下一步

下一步由用户通过 `scripts/start-web.ps1 -Gitea -Restart` 隐藏输入令牌，确认私有仓库列表和拉取。随后实现分支/提交选择、固定提交范围的一轮审查。保留现有引擎功能，按后续真实依赖逐步收敛 TUI、scan、delegate、OAuth 和 telemetry。

## 5. 技术栈决定

| 层次 | 方案 | 选择理由 |
| --- | --- | --- |
| HTTP | Gin | 满足成熟 Go Web 框架要求；使用路由分组、参数绑定、中间件和 HTML 渲染 |
| 页面 | Go `html/template` + CSS；按需加入 JS/SSE | 当前采用服务端渲染和普通表单，无前端构建链；执行进度阶段再加入动态交互 |
| 数据库 | SQLite + `database/sql` + 版本化 SQL migration | 单机数据随目录备份；关系、事务和历史查询清晰；首版不引入 ORM |
| SQLite 驱动 | `modernc.org/sqlite v1.60.1` | 纯 Go；Windows/Linux 构建不要求安装 C 编译器 |
| 实时消息 | 原生 EventSource / SSE | 单向推送任务和模型输出；用户提交使用 POST |
| 后台执行 | SQLite 持久化任务表 + 单个 worker | 不引入 Redis/MQ；重启可恢复队列，避免重复模型开销 |
| 静态资源 | `go:embed`，本地内嵌 | 单二进制分发，不依赖运行时 CDN |
| 日志 | `log/slog` + request/run/job ID | 本机定位错误；默认隐藏密钥和完整提示词 |

SQLite 启用 foreign keys、busy timeout，采用短写事务；WAL 允许读写并行但仍只有一个 writer，因此绝不在调用 LLM 时持有事务。数据库和产物放本机磁盘，不放网络共享目录。

已在 `go.mod/go.sum` 固定 Gin v1.12.0 和 SQLite 驱动版本。当前数据连接池仅使用一个连接，以简化个人平台的事务和并发行为。官方资料：[Gin](https://gin-gonic.com/en/docs/)、[SQLite WAL](https://sqlite.org/wal.html)。

## 6. 用户流程与页面

目标主流程：从 Gitea 导入仓库（或登记本地仓库）→ 创建 Review → 选择提交范围、填写背景 → 异步运行 → 查看 Diff 和问题 → 留言/追问 AI → 在 IDE 修改代码 → 创建下一轮 → 比较结果 → 归档。当前可完成仓库登记、草稿和归档；下表描述完整个人版的目标范围。

| 页面 | 首版功能 |
| --- | --- |
| 总览 | 最近 Review、执行中任务、未处理问题、按仓库/状态/标签筛选 |
| 仓库 | 添加规范化本地路径、检查 Git、分支/提交选择、重命名、归档 |
| 新建 Review | 标题、背景、基准/目标提交、规则、模型、预算预览和开始 |
| Review 工作台 | 左侧文件树，中间 Diff/行内问题，右侧讨论；顶部轮次切换、状态、重新审查 |
| 问题清单 | 严重性/类别/文件筛选，确认、忽略、解决、重新打开 |
| 轮次比较 | 新增、持续存在、已确认解决、未审查/无法确认；查看覆盖范围 |
| 历史和归档 | 搜索标题/问题/评论，恢复归档，导出 Markdown/JSON |
| 设置 | 模型 endpoint、模型名、密钥来源、规则和预算；无用户管理菜单 |

评论无需触发模型。提供明确的“向 AI 提问”动作，才创建消耗 token 的任务。普通评论、AI 问答和重新 review 是三个不同操作。

## 7. 核心数据模型

`Review` 是长期审查事项，`Run` 是该事项的一轮执行。Run 的输入和输出在结束后不可覆盖；重复 review 总是新建 Run。归档 Review 不删除历史。

| 表 | 关键字段/关系 |
| --- | --- |
| `repositories` | id、name、canonical_path（唯一）、git_common_dir、archived_at |
| `reviews` | id、repository_id、title、background、status、created_at、updated_at |
| `review_runs` | id、review_id、parent_run_id、sequence、mode、base_sha、head_sha、snapshot_hash、config_snapshot、status、coverage、usage、error、时间；unique(review_id, sequence) |
| `findings` | id、review_id、title、category、severity、triage_status、first_run_id；跨轮次稳定 ID |
| `finding_occurrences` | id、finding_id、run_id、old/new_path、side、line range、context_hash、模型原文、该轮观测状态 |
| `threads` | id、review_id、finding_id（可空）、起始 run_id、文件/代码锚点（可空）；支持总体讨论和行内讨论 |
| `messages` | id、thread_id、role、kind(comment/chat)、content、run_id、status、created_at；回复追加保存 |
| `jobs` | id、kind(review/chat)、run_id/thread_id、status、dedupe_key、attempt、error、started_at、finished_at |
| `events` | id（递增）、job_id、type、payload、created_at；SSE 回放和刷新恢复 |
| `tags`、`review_tags` | 个人标签分类；名称唯一，关联唯一 |
| `finding_events` | 问题状态变更、人工关联/解除关联、理由和时间；保留人工判断 |
| `schema_migrations` | 已应用版本与时间 |

外键和事务保证“创建 Run + 入队”原子提交。HTTP 写入采用幂等键；双击开始、网络重试不应创建两个同义任务。通过记录版本或条件更新避免多标签页覆盖状态。

密钥不存入 `config_snapshot`、events、日志或导出结果；首版从进程环境或本机受保护配置读取。页面只显示是否已配置。背景和规则快照保留文本或内容哈希用于回溯。

## 8. 复审与对话语义

### 复审

1. **重新运行同一份输入**：新 Run 指向相同快照，允许明确更换规则/模型，用于验证结论。
2. **审查修改后的代码**：新 Run 记录新 head/snapshot，通过 `parent_run_id` 关联上一轮。
3. **失败后重试**：新建有来源关联的执行记录；旧记录保留失败信息。只有输入和配置身份一致才允许使用原引擎 resume。

模型不再报告某个问题，不代表问题已经修复。按路径映射、类别、代码上下文等生成匹配候选，使用已有 Compare 作为基础；处理重命名、行号漂移和一对多匹配。没有覆盖的文件标记 `not_reviewed`。失败、取消、超预算、过滤范围变化都不能自动宣称问题解决。

人工 triage 状态与 AI 每轮观测状态分开：人工“忽略/解决”不被下一轮覆盖；重新出现时提示复查。匹配不确定时显示待确认，允许人工关联，不能仅凭行号继承旧评论。

### 对话

对话上下文由该线程绑定的 Run、选中问题、相关 Diff/源码快照、背景和线程历史组成。AI 回答注明引用的轮次和文件位置；用户明确切换到新轮次后才使用新快照。

对话是独立 ChatService，可复用 LLMClient 和只读工具，不直接重放 review agent 的全部内部工具消息。第一版每个线程一次只运行一个 AI 回答；普通评论仍可写入。保存用户消息、回答完成/中断状态和 token 使用量。长历史压缩后保留原消息，压缩结果只用于上下文预算。

## 9. 执行与输入一致性

首个可用版本只开放 commit/range 审查：入队时把 ref 解析为 SHA，确认范围含义并保存 Diff，读取代码也必须对应这些 SHA。不得入队后再读取已经移动的分支头。

通过应用数据目录下的独立快照或受管理 detached worktree 给引擎提供一致的源码视图。必须验证引擎、Git 查询和工具读取都只访问该快照。仅固定 Diff 而工具继续读取用户工作区不满足一致性。

workspace/staged 模式放到 P4：同时快照 index、工作区改动和允许纳入的未跟踪文件；冻结 Diff 与文件内容；不修改用户 index，不切换用户分支。大文件、二进制、忽略目录需明确跳过并计入覆盖报告。

队列状态：`queued → running → succeeded/failed/canceled/interrupted`。取消请求先进入 `cancel_requested`，确认底层停止后才终结。Run 可另有 `partial` 结果，保留已经得到的问题但不伪装完整成功。

- 默认一个 LLM job worker；引擎内部并发也设置保守上限和 token/time budget。
- job context 属于服务生命周期，不使用提交请求的 context；浏览器关闭不取消任务。
- 重启将遗留 running 标记 interrupted；queued 可继续，interrupted 不自动再次收费执行。
- 网络重连按 `Last-Event-ID` 回放事件，完成后关闭 SSE；定期聚合 token 增量，避免逐 token 写数据库。
- 取消、超时、panic、磁盘写入失败都落盘为可见状态，释放占用的 worker。

## 10. 代码组织和接口边界

当前基础版保持一个小包，不预建空目录：`config.go` 管理启动参数和设置校验，`store.go` 管理数据访问和 migration，`server.go/forms.go` 管理页面路由和表单，`ui/` 保存内嵌页面。以下目录是执行、评论和复审加入后的目标拆分，按实际复杂度逐步提取。

```text
cmd/ocr-web/                  新 Web 程序入口：参数、组装、启动与退出
internal/platform/
  domain/                    Review/Run/Finding/Thread/Job 类型与状态约束
  store/                     SQLite、migration、事务和查询
  repository/                仓库登记、ref 解析、快照
  review/                    ReviewService、引擎适配器、结果归档和匹配
  chat/                      ChatService、上下文组装、只读工具策略
  jobs/                      持久化队列、worker、取消与恢复
  web/                       Gin 路由、handler、middleware、DTO、SSE
  ui/                        内嵌 templates/static，本地资源
internal/agent, diff, llm...  保留并逐步适配的上游核心
```

HTTP handler 只校验输入、调用 service 和返回结果；数据库事务在 service/store 层；引擎适配器输出结构化 result/event，不解析终端文本。先定义小接口，例如 `ReviewEngine.Run(ctx, input, emit)`，用假的 engine 验证队列与平台，不在测试里真实调用收费模型。

`cmd/opencodereview/shared.go` 的配置解析/工具组装逐项提取到可复用包。每次提取保持 CLI 行为和测试通过，避免一口气重构整个引擎。TUI、OAuth、MCP、scan、delegate、telemetry 可暂留代码；Web 不暴露这些入口，待核心依赖图明确后第二轮精简。

计划 API：

| 方法/路径 | 用途 |
| --- | --- |
| `GET/POST /api/repositories` | 列表、添加本地仓库 |
| `GET /api/repositories/:id/refs` | 可选择的本地分支和提交 |
| `GET/POST /api/reviews` | 筛选列表、创建事项 |
| `GET/PATCH /api/reviews/:id` | 详情、标题/背景/归档 |
| `POST /api/reviews/:id/runs` | 新一轮审查，返回 202 + job ID |
| `GET /api/runs/:id` | 状态、输入、覆盖、结果和费用信息 |
| `GET /api/reviews/:id/compare?before=...&after=...` | 同事项轮次比较 |
| `POST /api/jobs/:id/cancel` | 请求取消 |
| `GET /api/jobs/:id/events` | SSE，带事件 ID |
| `POST /api/reviews/:id/threads` | 创建总体/行内讨论 |
| `GET/POST /api/threads/:id/messages` | 历史和普通留言 |
| `POST /api/threads/:id/chat` | AI 提问，返回 202 + job ID |
| `PATCH /api/findings/:id` | 人工 triage，附理由 |
| `GET /api/reviews/:id/export` | Markdown/JSON 导出 |

分页和请求体大小有上限；跨 Review 的 run/finding/thread 组合返回错误，即使没有多用户也必须防止串数据。

## 11. 本机应用约束

- 启动时拒绝非 loopback 地址；沿用 viewer 的 Host 校验思路，拒绝任意 Host、跨站写入和跨域 API 访问。
- 写请求校验 Origin/CSRF；只读 GET 不触发模型执行或数据变更。
- 使用模板自动转义；Markdown 默认禁用原始 HTML，并对链接协议做限制。模型文本和代码都不能直接当成可信 HTML。
- 仓库路径必须规范化和验证；读取文件必须落在登记仓库或应用快照目录内，覆盖符号链接、Windows junction 和路径穿越测试。
- Git 命令使用参数数组，不拼接 shell 字符串；Web 首版不开放任意 shell/MCP 工具或仓库指定的可执行 key command。
- 旧配置导入只允许明确字段，不自动执行仓库中的外部指令。
- 数据目录独立于源码目录，包含数据库、快照、产物、日志；加入 ignore。备份使用 SQLite 一致性备份或停机完整备份，不能运行时只复制 `.db` 而忽略 WAL。

## 12. 分阶段交付与验收

以下为单人开发的粗估工作日，不是固定工期；Go 环境、上游测试基线、快照兼容性可能影响估算。每阶段先满足验收再进入下一阶段。

| 阶段 | 工作 | 预估 | 验收标准 |
| --- | --- | --- | --- |
| P0 基线 | 外围删除已完成；完成检查与回归验证 | 1–2 天 | 本机 build/check/test 通过；核心测试保留；没有失效运行路径 |
| P1 Web 和存储 | 已实现 Gin、SQLite migration、loopback/Host/CSRF、内嵌页面、仓库/Review 草稿、设置和 Gitea 导入/同步；待用户配置令牌完成真实联调 | 2–3 天 | 一条命令启动；添加仓库、创建/归档 Review；重启数据保留；没有用户体系 |
| P2 审查闭环 | 配置抽取、engine adapter、commit/range 快照、持久化 job、SSE、Diff/finding 展示、取消 | 3–5 天 | 浏览器发起真实 review 并看到可追溯结果；关闭页面后任务继续；失败可见且可手动重试 |
| P3 评论和对话 | 总体/行内 thread、普通评论、AI 上下文与流输出、费用记录 | 2–4 天 | 同一问题可多次往返；刷新后消息完整；回复引用正确轮次；留言不会自动调用模型 |
| P4 多轮复审 | 新 Run、比较、问题跨轮匹配、人工 triage、workspace/staged 快照 | 3–5 天 | 改代码再审不覆盖历史；重命名不串评论；未覆盖/失败不误报已解决 |
| P5 整理与日用 | 搜索/标签/导出、旧 JSONL 导入、备份恢复、Windows 打包、UI 打磨 | 2–3 天 | 完整日常流程可独立完成；数据恢复验证通过；离线静态资源可用 |

完整个人版粗估 13–22 个工作日。P1+P2 形成可用的 Web 审查入口，P3+P4 完成这次需求的核心闭环。

首个开发切片和 Gitea 导入/同步代码已实现；接下来完成私有仓库接入确认、分支/提交选择，再实现固定提交范围的一轮执行和持久化讨论。

## 13. 后续验证范围

按当前用户要求，本轮不新增或运行测试。下列是功能逐步接入时需要关注的行为，不作为本轮执行测试的要求。

- 存储：migration 从空库/旧版本升级、外键、事务回滚、幂等提交、重启持久化。
- 队列：双击只入队一次、崩溃中断恢复、queued/running 取消、失败释放 worker、SSE 重连不丢最终状态。
- 引擎：fake LLM/fake engine 覆盖成功、超时、额度耗尽、部分结果；真实模型只做手动低预算验证。
- 快照：任务排队后用户修改工作区/切分支，运行输入不改变；工具读取与 Diff 一致。
- 复审：新增/持续/未覆盖/候选解决，重命名、行号漂移、不确定匹配，人工状态不被覆盖。
- 对话：线程隔离、轮次绑定、错误恢复、上下文裁剪、普通留言不产生模型任务。
- Web：非 loopback 启动失败，Host/CSRF/XSS/路径越界，请求体与分页限制。
- 交付：直接使用 Go 命令构建；需要验证时运行静态检查、单元测试和覆盖率检查（目标 90%），遵循用户是否执行测试的要求；验证 Windows/Linux 运行；固定依赖并保留 LICENSE。
- 最终手工验收：添加仓库 → 审查 → 留言 → 两轮 AI 对话 → 修改代码 → 复审 → 对比 → 关闭/重启 → 历史仍完整 → 导出并恢复备份。

如后续提交或开 PR，遵循 AGENTS.md：提交前运行指定的 `ocr review`，提交说明用英文，不添加 AI attribution trailers；PR 中披露使用 AI 与工具/模型，发起人工 review 前由本人核对生成内容。
