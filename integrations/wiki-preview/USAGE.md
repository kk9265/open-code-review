# Wiki 函数说明模块

## 正文生成与试点边界

**助手不得自行撰写、翻译、补写或改写 Wiki 正文。正文必须由 Wiki 模块调用模型生成。** 用户已明确授权助手维护模块、执行 LoginServer / LoginWhiteList 试点命令，并围绕该试点补充跨类调用关系与业务过程。其他类仅作为这条业务链的源码上下文，不单独生成页面；用户确认可读性前，不扩展其他业务。

**最新授权：用户已确认试点可读性，并允许解开执行入口、补充 LoginServer 其余模块。归组、需要补读的源码选择、正文和正文修正都必须由 Wiki 模块调用 AI Mana 的 DeepSeek（当前 `deepseek-flash`）完成。助手只维护执行机制并运行命令，不能代替 DS 写 Wiki。AccountServer 仍不在本轮范围。** 下文旧试点限制不应阻止这次已明确授权的 LoginServer 扩展，也不构成其他服务的授权。

## LoginServer 剩余模块执行

```powershell
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --remaining-modules --dry-run
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --remaining-modules --concurrency 3
```

新入口独立于旧文件页管线：先自动提取冻结源码，再由 DS 按职责归组；每个剩余函数恰好归属一个模块，所有非脚本文件必须有归属，`.h/.cpp` 不拆成两份说明。DS 可请求补读完整的跨类函数、头文件和协议定义，模块核对路径、精确符号及预算；缺失、未展开、解析不确定与运行时边界分别保留，禁止猜补。

业务页只提供业务关系、文字过程、最终结果和必要缺口，不列函数清单。“类与函数”是独立导航入口，不能把所有说明合并到一篇“函数索引”长文档。按冻结源码中的类归属组织，自由函数按源文件归组；每个函数独立阅读。类目录只提供签名、已有模型摘要与链接。模型说明原文迁移，只调整标题层级和结构性跳转地址。简短函数有更少步骤和可选语义；不生成流程图或证据栏目。

历史全局函数页可用 `.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/function_pages.py --scope LoginServer` 预检查结构迁移；加 `--publish` 才发布。该命令不调用模型、不生成或改写说明，已迁移则直接报告完成。当前 LoginServer 已迁移，无需重跑或重新生成。

执行固定使用本机 AI Mana 的 `deepseek-flash`，不改 Review 设置、不允许回退为助手撰写。归组和函数格式输出使用非思考模式，其余步骤低推理；所有返回都须通过结构校验和模型源码检查。检查点绑定精确请求、源码和提示词，可用于失败后续跑；所有目标通过后才原子发布，失败保留当前页面。已完成同一冻结提交的扩展命令重复执行不会重生成。

2026-10-19 已清空旧 LoginServer / AccountServer 正文、发布历史与正文中间结果。认可试点后，LoginServer 已扩展至 10 个业务页、28 个轻量类或源文件目录和 191 个独立函数页（其中新增函数 184 个）。此前 11 页布局中的全局函数正文长页已拆除，业务页和所有函数解释原文保留。AccountServer 保持为空。旧的 114 页 / 53 页统计和 AccountServer 中文修复状态均已失效，不得据此恢复或补生成。

阅读入口：<http://127.0.0.1:5484/wiki?scope=LoginServer>。试点页：<http://127.0.0.1:5484/wiki/scopes/LoginServer/read/page/class-b32ed152dddbea81c404>。

## 页面与生成结构

同一个类的 `.h/.cpp` 归入同一个类目录，声明与实现对应同一份函数说明，重载使用独立身份、页面和锚点。“业务过程 / 类与函数”两个入口分别阅读业务和函数，函数页提供作用、文字流程和有意义的边界处理；不会把整类的全部说明堆进一篇正文。

调用前提、参数/返回约定、状态变化、所有权、执行时序、并发、重试、关键依赖与不确定性按需展示。简单函数不要求填满栏目；类层面的共享信息不应在每个函数里重复。页面不生成流程图或“证据”“源码依据”章节，源码位置以“查看声明 / 查看实现”链接提供。

`cpp_context.py` 从固定提交读取完整函数体、声明、成员信息、直接类内依赖和可取得的直接 include。无法匹配完整实现、解析有歧义或超预算时停止，不把截断片段当作完整函数。不可用依赖显式记录，模型不得补写其内部行为。

`generate_functions.py` 使用固定 CodeWiki 的模型网关调用本机 AI Mana。类概述和每个函数的说明均由模型返回；模型再对照源码检查，必要时在模块内有限修正。结构、函数覆盖与源码对照检查通过后才统一发布，状态仍为 `awaiting_user_review`，不表示用户已经认可可读性。

说明的步骤、边界和额外语义数量由函数规模控制；篇幅检查区分说明文字与行内代码标识符。装配器只负责固定 UI 标题、索引和源码链接，不补写解释性正文。

旧 `--with-business` 入口已退役，固定业务组、函数选择清单及专用上下文构建器已删除。业务归组和补读源码由 `--remaining-modules` 流程调用 DeepSeek 决定，程序负责解析与校验。共用函数解析器位于 `cpp_context.py`。已发布正文和历史检查点保留；此次清理不触发重新生成。调用表达式只表示语法关系，不能自动视为已执行的直接调用。

`business_guide.py` 调用同一 Wiki 模型网关生成各过程，再逐段对照源码检查。必要修正仍由模型完成；检查不通过时保留当前发布。每段的调用关系、步骤、结果及按需出现的未确认环节有数量与篇幅上限；不增加流程图和证据栏目。装配仅使用模型原文与固定 UI 标题、导航、源码链接。原有函数说明可复用同源码、提示词和模型的验证缓存。

缺失源码须经过冻结 Git 树的相对路径与后缀查找，不能把当前上下文未选入的可用实现说成缺失。缺少生成的 RPC 头文件也不等于缺少 `.sdp` 定义或服务端实现。源码缺失、试点未展开和运行时路由/送达无法确认分别记录；异步提交不能表述为下游成功完成或持久化成功。

业务请求给 DeepSeek 明确设置 `reasoning_effort=low` 并保留足够响应额度，避免推理耗尽额度后只返回空正文；此参数来自 [DeepSeek 官方接口说明](https://api-docs.deepseek.com/guides/thinking_mode/)。这不改变 Review 配置或 Wiki 的模型选择。空响应仍按失败处理，不能以推理内容代替正文。检查点只在请求上下文与提示词精确一致、模型来源一致且当前结构校验通过时复用。

## 历史试点命令

以下是原 LoginServer / LoginWhiteList 试点入口。扩展发布后不要运行单类命令覆盖当前范围，也不要为了绕过保护清空其他页；剩余模块执行使用上方新入口。接手或普通阅读均不构成自动重跑授权。

```powershell
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --class-name LoginWhiteList --header LoginServer/LoginWhiteList.h --implementation LoginServer/LoginWhiteList.cpp --dry-run
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --class-name LoginWhiteList --header LoginServer/LoginWhiteList.h --implementation LoginServer/LoginWhiteList.cpp --concurrency 1
```

`--dry-run` 仅检查符号归组、实现覆盖及所选业务上下文，不加载模型、不写正文。正常命令支持请求缓存与失败后续跑；只有上下文、提示词、模型一致且通过当前校验的模型结果才可复用。旧的文件页/英文修复管线不是当前试点路径，普通批量生成默认已禁用。

已有业务过程的页面，单类函数生成命令会拒绝降级覆盖，避免维护时丢失跨类业务层。旧 `--with-business` 参数会在读取源码或调用模型前明确报错。试点仍是有界源码阅读，调用方覆盖不代表整个系统已被证明完整；动态路由与真实运行结果以正文明确的边界为准。

## 阅读与维护

Review、Wiki 阅读和历史目录预览共用 Go 服务的 5484 端口。`/wiki/read/` 与 `/wiki/scopes/<scope>/read/` 读取本地发布清单，不调用模型。函数支持侧栏搜索、锚点定位、查看声明/实现和返回原函数；旧页面已删除时，不再恢复失效的阅读位置。

正文数据在 `.ocr-data/wiki-loginserver-generated/`、`.ocr-data/wiki-accountserver-generated/`。模型取对应目录的 `generation-model.json`，当前试点为 AI Mana 的 `deepseek-flash`，与 Review 设置独立。源码固定在 `.ocr-data/wiki-zgame-plan/zgame`，提交为 `9052b9e77e681315a200a8489dfc6938d81024db`。阅读无需 Mermaid，也无需启动旧 Python 阅读服务。

构建、启动：

```powershell
python scripts/toolchain/go.py build -o dist/ocr-web.exe ./cmd/ocr-web
python scripts/workbench/start-web.py
```

已经运行时不要重复启动，Windows 不能直接覆盖运行中的二进制。替换服务前须确认实际 PID、无活动审查任务并保留凭据，不能输出凭据。

清空工具为 `reset_generated.py --discard-all-generated`，只在用户明确要求丢弃旧正文时使用，不用于接手、检查或普通维护。它删除两个服务的生成正文、数据库、历史和中间结果，保留模型配置、本地工具、共享源码和代码图；新试点不会恢复清空前的正文。

## 历史预览工具参考

下面记录旧的零模型调用差异/影响预览与文件目录快照。它们继续只读可查，但不决定当前函数指南的归组与正文结构，也不构成任何生成授权。

## 固定的上游版本

- CodeWiki：`7be8f702504cbd69aec6491a2e4e81f5311e0ba6`，MIT 许可证。
- `patches/0001-incremental-graph.patch`：完整恢复解析数据、修正配置文件哈希、兼容旧索引，并从模型上下文排除解析缓存。
- `patches/0002-code-catalog-plan.patch`：扩展文件目录、职责拆页与复杂功能候选规划。
- `patches/0003-chinese-generation-defaults.patch`：将生成默认语言及提示模板改为简体中文。
- `vendor-lock.json`：记录提交、补丁、依赖清单和修复后文件的 SHA256。
- `requirements-windows-py312.lock`：此前实际验证的 Windows CPython 3.12.12 图分析环境版本。它不是跨平台锁文件，也不包含分发包的哈希；预览模块本身不安装、不加载这些依赖。

从现有本地克隆准备固定源码，不联网、不安装包：

```powershell
python integrations/wiki-preview/prepare.py --source <CodeWiki本地克隆> --destination .ocr-data/codewiki-pinned
```

目标目录必须不存在。仅校验时省略 `--destination`。使用已安装图分析依赖的 Python，加 `--verify-runtime` 可核对运行环境。助手不得运行上游 `wiki`、`watch` 或带默认生成行为的 `update` 命令；环境维护不构成生成授权。

## 预览流程

在仓库根目录运行：

```powershell
python integrations/wiki-preview/worker.py enqueue --repo . --branch main --baseline <旧提交SHA> --catalog <旧版manifest.json>
python integrations/wiki-preview/worker.py work-once
python integrations/wiki-preview/worker.py jobs
python integrations/wiki-preview/worker.py current
python integrations/wiki-preview/worker.py show <snapshot_id>
```

`--commit` 可指定分支历史中的目标提交，省略时在入队时固定分支头。`--baseline` 是显式比较基线，不会自动变成上一个预览提交。不指定基线时视为首次建立预览。`--scope internal/platform` 可重复传入，用于限制范围。未提交修改不参与预览。

默认数据目录是 `.ocr-data/wiki-preview/`。可在子命令前加 `--state <独立目录>`。队列数据库名固定为 `wiki-preview.sqlite3`，不使用审查任务表、审查迁移或审查 worker。`work-once` 每次只处理一个任务，没有后台轮询、Webhook 或自动生成。

支持上次验证导出的 CodeWiki `manifest.json`；也支持最小引用清单：

```json
{
  "commit": "完整的基线提交SHA",
  "pages": [
    {"slug": "overview", "global": true, "source_files": []},
    {"slug": "queue", "parent_slug": "overview", "source_files": ["internal/platform/queue.go"]}
  ]
}
```

清单的提交必须与基线相同，引用数据会在入队时保存，之后修改清单不会改变已有任务。语言默认 `zh`，可通过 `language_code` 区分。

## 队列与版本规则

- 仓库本地绝对路径定义仓库身份；同一远程的不同克隆暂视为不同仓库。
- 相同仓库、分支、提交、基线、范围、引用清单、补丁和预览实现不重复入队；失败任务再次提交会建立新尝试。
- 同分支的新请求合并尚未执行的旧请求，旧请求保留为 `superseded`，不是删除记录。
- SQLite 事务保证原子领取。任务租约为 300 秒；崩溃后的任务可在租约到期后被下一次 `work-once` 重新领取。过期 worker 不能完成新领取者的任务。
- 快照身份包含仓库、分支、提交和配置，内容写入后不可覆盖，并校验 SHA256。文件使用扁平哈希路径，避免 Windows 路径过长；版本信息保存在独立索引和快照中。
- 成功才切换 `current_previews` 指针；新请求已入队时，较早任务完成也不能覆盖新请求的预览。失败保留已有指针。
- 此处发布的是**预览快照**，不是 Wiki。快照包含 Git 树中的路径及 blob 标识、差异和影响计划，不复制源码，不生成正文。即使源仓库后续变化，已保存预览仍可读取。

## 可选代码图输入

可以显式传入固定版 CodeWiki 格式的本地 SQLite 图数据库。模块只读已有图，不启动分析器、不重建索引、不加载 CodeWiki 或任何模型 SDK：

```powershell
python integrations/wiki-preview/worker.py enqueue --repo . --branch main --baseline <旧提交SHA> --catalog <旧版manifest.json> --baseline-graph <基线图.sqlite3> --target-graph <目标图.sqlite3>
python integrations/wiki-preview/worker.py work-once
```

- 基线图对应 `--baseline`，目标图对应入队冻结的目标 SHA。两侧分别查找依赖，保留删除前和重命名前的关系。首次预览没有基线时只使用目标图。
- 每个数据库必须恰有一个仓库记录匹配该 SHA，最近一次分析状态为 `done`；图内所有文件/config 节点的 SHA256 必须与该提交的 Git blob 一致，符号节点的哈希也须匹配所属文件。CRLF 工作副本与 LF Git blob 不一致时会拒用图，应从准确的 Git 内容建立索引。
- 以只读事务读取节点、边和完成状态，入队时保存规范化图内容及指纹。之后删除、修改原图或分支前进，不影响排队任务；图内容参与去重和版本身份。快照只保存影响链与图摘要，不复制完整图或源码。
- 缺图、提交不符、文件哈希不符、未完成分析或损坏数据库，均降级为直接引用预览，原因显示在 `graphs`。图未覆盖的变更路径列入 `unindexed_changed_files`。不会自动调用模型补齐。
- 当前使用固定上游版本的数据库结构，但数据库没有可信的生产者版本证明；结果保留 `producer_version_unattested`。文件哈希检查能排除源码版本混用，不能证明静态解析的完整性或正确性。

CodeWiki 的文件节点保存原始字节哈希，符号节点可能保存 UTF-8 解码（替换非法字节）、统一换行后的文本哈希。校验时先要求文件原始 SHA256 与冻结 Git blob 完全一致，再允许符号哈希匹配该 blob 的原始或规范化文本哈希。不会忽略任意哈希差异，也不改写源文件或图中哈希；这仅兼容上游的读取语义，不证明非 UTF-8 注释的语义正确。

## 影响精度

预览 schema 2 保留增删改与重命名、父页面和新增目录建议，并区分：

- `affected_pages[].impact=direct`：页面直接引用变更文件，或显式标为全局摘要。
- `impact=indirect`：页面引用的文件沿静态依赖链到达变更文件。`dependency_evidence` 包含图指纹、比较侧、准确提交、逐跳文件、符号/行范围、原始边、置信度及解析依据。
- `impact=parent`：因子页面受影响而需要检查的父页面。
- `potentially_affected_pages` 和 `unknown_page_details`：没有找到静态路径的其余页面，仍无法判断，不能当作已验证可复用。

依赖按“使用方 → 被依赖方”反向传播，支持 `calls/references/imports/inherits/implements/routes_to/uses_config`。目录包含、定义和导出结构边不传播。使用文件粒度的保守传播，文件里一个符号受影响时可能涉及该文件的其他符号；结果是待检查候选，不证明页面内容必然需要修改。`--scope` 限制变化起点，依赖链仍可穿过范围外文件。

每个比较侧、每个受影响文件保留一条代表链：优先选择所有边置信度均至少 0.8 的链，再选择较短链。`resolution=resolved` 仍是静态解析结果；包含较低置信度边时为 `inferred`，逐边 `is_inferred` 和原始依据始终保留。动态调用、反射、未解析引用等可能漏检，即使两侧图均可用也不将未命中页面标为安全复用。净变化为空时才将原有页面列入 `unchanged_pages`。

缺少清单时返回 `catalog_required=true`，不虚构目录或页面。token 数保持未知，实际计划模型调用数始终为 0。目录建议需要后续校核，不会自动创建页面。

## 独立只读预览页面

从项目根目录启动，只监听本机 `127.0.0.1:5488`：

```powershell
python integrations/wiki-preview/viewer.py --state .ocr-data/wiki-preview --port 5488
```

然后打开 <http://127.0.0.1:5488/>。命令在前台运行，Ctrl+C 停止；`--state` 应选择保存所需预览快照的独立状态目录。查看本次已经验证的真实依赖链样例：

```powershell
python integrations/wiki-preview/viewer.py --state .ocr-data/wiki-preview-validation/graph-impact/state --port 5488
```

页面提供：

- 历史快照切换、当前版本标记、基线/目标 SHA 和变化起点范围；刷新仅重新读取快照。
- 按直接影响、间接候选、父页面、无法判断、范围内无变化分类；按页面名称、源码路径及依赖证据搜索。
- 页面引用的源码、影响原因，以及逐跳依赖链；可展开符号和行范围、边 ID、原始解析依据、置信度和图指纹。基线与目标证据分别标识。
- 图不可用、未索引变更路径、缺少清单和读取失败的提示，以及经完整性校验的快照 JSON。

这是独立 Python 标准库 HTTP 服务，不依赖 Go 工作台，不读取工作台数据库、路由或模型设置。服务以 SQLite `mode=ro` 和 `query_only` 读取既有预览库，不创建状态目录或数据库、不领取任务、不运行 Git/分析器，不提供入队、执行或生成入口。快照哈希不符或不属于零模型调用预览时拒绝展示。空目录显示引导提示；schema 1 历史预览也能读取，但没有依赖链的旧快照不会凭空补充证据。

静态资源全部由本机服务提供，无 CDN、外部字体或远端请求。中文文案独立放在 `ui/i18n/zh-CN.json`；使用 JSON Unicode 转义保存，以符合独立模块的英文源码检查，不修改现有检查器豁免规则。界面显示正常中文。

## 历史只读规划：文件与复杂功能双目录

此模式是固定 CodeWiki 的二次开发，核心实现位于补丁 `patches/0002-code-catalog-plan.patch`，扩展其 `WikiAgentWorkflow.plan_code_catalog` 和 `wiki/catalog/code_plan.py`。生成默认语言与模板另由第三份补丁调整为简体中文。`prepare.py` 按锁文件顺序应用三份补丁，校验扩展源码哈希；旧的已准备副本需要按新锁重新准备。运行时只加载已校验的纯规划模块，不导入 CodeWiki 的模型网关、数据库服务或生成器。

```powershell
python integrations/wiki-preview/prepare.py --source <CodeWiki本地克隆> --destination <新的固定版副本目录>
python integrations/wiki-preview/catalog_preview.py --repo . --branch main --commit <目标SHA> --graph <该SHA的CodeWiki图.sqlite3> --codewiki-source <新的固定版副本目录>
python scripts/workbench/start-web.py
```

打开 <http://127.0.0.1:5484/wiki?view=plan>（工作台已启动时无需重复运行启动命令）。默认状态目录 `.ocr-data/wiki-code-plan` 与原来的影响预览分开；其独立 `wiki-preview.sqlite3` 只存规划任务和不可变快照，不是 CodeWiki 正文数据库。没有调用 `save_doc_catalog` 或 `upsert_doc_page`，初始 Wiki 的目录和正文均保持原样。

规划采用完整冻结提交，不依赖提交差异：

- **文件目录**：按源码路径建立文件总览；同名文件有独立稳定 ID，无 110 项截断。常见 Go/C/C++/Python/JS/TS/Java/Kotlin/Rust 等源码扩展名及 SQL 参与规划，但须先应用前述脚本过滤规则，业务 Python 保留。其他扩展名记录在 `excluded_files`，不能声称已覆盖整个 Git 树。`--scope` 可限制文件范围。已有规划快照保留历史内容，不因过滤规则更新而重写。
- **职责拆页候选**：默认达到 300 行或 12 个索引符号时尝试拆页，可用 `--large-lines` / `--large-symbols` 调整。依据限定符中的声明归属和名称主题词分组，不机械切行；保留每组符号和原始行范围。单个巨型符号或缺少分组依据时明确待人工拆分，不虚构职责。索引符号分配数量不代表行为完整性已验证。
- **复杂功能候选**：复用已有 CodeWiki 的调用、路由、导入、继承、实现和配置关系，从入口沿有向依赖查找跨文件代码；入口不明显的循环也作为候选。每个候选保留符号、边和到文件页的链接，文件页也能返回相关功能。
- 功能候选优先入口端点及 main/run/start/execute/handle 类入口，默认展开 80 个（`--feature-limit` 可调整）。其他候选入口记录在 `deferred_feature_entries`，页面显示数量和清单，不静默当作已覆盖。每个候选调查最多 4 跳、80 符号或 160 条边，截断时显式提示。
- 静态解析链仍可能包含推断，尤其全局同名符号匹配。职责主题词不证明真实职责、依赖可达性不证明业务边界；所有拆页和功能均为 `proposed/review_required`。本阶段不命名或生成未经源码语义核实的业务说明。

规划要求提交匹配、分析完成且文件哈希校验通过的现有图。没有图时拒绝规划，不自动建立索引或调用模型。文件行数直接来自冻结 Git blob；页面搜索、目录展开、双视角跳转和 JSON 阅读均为只读。schema 3 规划与旧 schema 1/2 影响预览分别展示。

## 验证

### 历史 zgame 规划实例与生成策略记录

已对 `zgame/master` 的冻结提交 `9052b9e77e681315a200a8489dfc6938d81024db` 运行独立规划，快照为 `39de705b8385fa00e1738069f8655cb1c9287cb9a77b1552192d3bf29a3ceace`。在 <http://127.0.0.1:5484/wiki/plan/planning#39de705b8385fa00e1738069f8655cb1c9287cb9a77b1552192d3bf29a3ceace> 查看。

该轮使用 `.ocr-data/wiki-zgame-plan/zgame` 的独立检出，静态图为同目录旁的 `graph.sqlite3`，只写独立图与规划状态。检出通过本地对象 alternates 引用托管 bare 库以避免复制整个历史，不修改源仓库；如以后重建图，仍依赖该本地对象库可用。已经保存的规划快照阅读不依赖源检出。

该历史预览记录的 `generation_policy` 为：目标服务 AI Mana、当时审查模型 `glm-5.3`、审查设置版本 4、`assistant_authorship=false`、`generation_enabled=false`。该轮未调用、未验证正文生成接口，这些记录不证明当前模型连接或配置。Wiki 模型以对应产物目录的 `generation-model.json` 为准，与 Review 设置独立。当时要求由用户操作生成工具，禁止助手执行、验证或发布生成任务；该历史限制已被本轮明确的 LoginWhiteList 试点命令授权更新，仅限本文开头列出的试点范围。**助手始终不得自行撰写、翻译、补写或改写 Wiki 正文，所有正文仍必须由 Wiki 模块调用模型生成。** 历史策略与命令示例不构成其他业务的生成授权。

本次模型调用 0，独立库的 `llm_run/doc_page/doc_catalog` 均为 0。2 项解析失败和未入图项保留在快照中，不能宣称已完整理解所有源码。初始 Wiki 和审查流程保持不变。运行摘要见 `.ocr-data/wiki-zgame-plan/validation.json`。

### 模块回归检查

```powershell
python scripts/checks/run-tests.py --suite wiki
```

测试覆盖队列去重、合并、竞争领取、租约恢复、乱序完成、失败重试、分支与配置隔离、不可变快照、篡改检测、引用基线、重命名、中文文件路径及禁网执行；另覆盖依赖传播方向、多跳/循环、两侧删除/重命名、置信度择路、跨范围传播、图冻结及缺图/错版/未完成/损坏/覆盖不足降级。测试使用临时 Git 仓库，不操作审查服务。
