# Wiki 预览模块

## Review 统一 Wiki 入口

工作台入口为 <http://127.0.0.1:5484/wiki>，同一页签下切换「Wiki 正文／目录规划」。浏览器仅访问 Review 同源的 `/wiki/read/`、`/wiki/plan/` 只读代理；内部正文服务 5491 与规划服务 5490 不再需要作为独立入口打开。代理不转发工作台 Cookie/Authorization，不提供生成或写入路由；内部服务停止时显示不可用提示，不会触发模型或自动生成。

当前正文为 zgame 冻结版本的 LoginServer：17 个文件页、88 个职责子页、9 个复杂功能页。左侧按文件挂接职责子页，或切换复杂功能目录；支持搜索、面包屑和当前页面标记。正文页面、目录选择、搜索和展开状态按仓库/提交保存在浏览器；规划快照、页面选择及搜索分别保存，切换视图后可恢复。源码引用在同页阅读区域定位行范围，并可返回原正文；Mermaid 和源码依据折叠继续生效。

只读正文服务已从临时运行脚本整理为可维护源码：

```powershell
python integrations/wiki-preview/generated_viewer.py --state .ocr-data/wiki-loginserver-generated --port 5491
python integrations/wiki-preview/viewer.py --state .ocr-data/wiki-code-plan --port 5490
```

正文阅读服务需要当前已验证运行环境的 `markdown-it-py`，Mermaid 运行资源位于 `.ocr-data/wiki-mermaid/node_modules/mermaid/dist`（固定 `mermaid@11.15.0`，安装时禁用脚本）。这些是渲染依赖，不是模型依赖。阅读服务只读取已生成的 manifest/Markdown，不参与正文生成、不覆盖初始 Wiki，也不修改审查流程。

新环境准备渲染依赖（已有依赖时不需重复安装）：

```powershell
python -m pip install -r integrations/wiki-preview/requirements-reader.txt
npm install --prefix .ocr-data/wiki-mermaid --cache .ocr-data/wiki-mermaid/npm-cache --ignore-scripts --no-audit --no-fund --save-exact mermaid@11.15.0
```

规划模块独立于审查服务，提供本地 Git 差异和可选代码图依赖影响预览，没有模型调用、Wiki 正文生成、Wiki 发布或自动启动入口。规划运行时仅依赖 Python 标准库和本地 Git，不读取工作台模型设置或审查数据库。新增正文阅读服务只渲染已有结果，其渲染依赖见上节。

## 固定的上游版本

- CodeWiki：`7be8f702504cbd69aec6491a2e4e81f5311e0ba6`，MIT 许可证。
- `patches/0001-incremental-graph.patch`：完整恢复解析数据、修正配置文件哈希、兼容旧索引，并从模型上下文排除解析缓存。
- `vendor-lock.json`：记录提交、补丁、依赖清单和修复后文件的 SHA256。
- `requirements-windows-py312.lock`：此前实际验证的 Windows CPython 3.12.12 图分析环境版本。它不是跨平台锁文件，也不包含分发包的哈希；预览模块本身不安装、不加载这些依赖。

从现有本地克隆准备固定源码，不联网、不安装包：

```powershell
python integrations/wiki-preview/prepare.py --source <CodeWiki本地克隆> --destination .ocr-data/codewiki-pinned
```

目标目录必须不存在。仅校验时省略 `--destination`。使用已安装图分析依赖的 Python，加 `--verify-runtime` 可核对运行环境。未经单独授权，不运行上游 `wiki`、`watch` 或带默认生成行为的 `update` 命令。

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

## CodeWiki 文件与复杂功能双目录规划

此模式是固定 CodeWiki 的二次开发，核心实现位于补丁 `patches/0002-code-catalog-plan.patch`，扩展其 `WikiAgentWorkflow.plan_code_catalog` 和 `wiki/catalog/code_plan.py`。现有默认目录和正文生成流程不变。`prepare.py` 依次应用两份锁定补丁，校验扩展源码哈希；旧的已准备副本需要按新锁重新准备。运行时只加载已校验的纯规划模块，不导入 CodeWiki 的模型网关、数据库服务或生成器。

```powershell
python integrations/wiki-preview/prepare.py --source <CodeWiki本地克隆> --destination <新的固定版副本目录>
python integrations/wiki-preview/catalog_preview.py --repo . --branch main --commit <目标SHA> --graph <该SHA的CodeWiki图.sqlite3> --codewiki-source <新的固定版副本目录>
python integrations/wiki-preview/viewer.py --state .ocr-data/wiki-code-plan --port 5490
```

打开 <http://127.0.0.1:5490/planning>。默认状态目录 `.ocr-data/wiki-code-plan` 与原来的影响预览分开；其独立 `wiki-preview.sqlite3` 只存规划任务和不可变快照，不是 CodeWiki 正文数据库。没有调用 `save_doc_catalog` 或 `upsert_doc_page`，初始 Wiki 的目录和正文均保持原样。

规划采用完整冻结提交，不依赖提交差异：

- **文件目录**：按源码路径建立文件总览；同名文件有独立稳定 ID，无 110 项截断。常见 Go/C/C++/Python/JS/TS/Java/Kotlin/Rust 等源码扩展名及 SQL、脚本参与规划，其他扩展名记录在 `excluded_files`，不能声称已覆盖整个 Git 树。`--scope` 可限制文件范围。
- **职责拆页候选**：默认达到 300 行或 12 个索引符号时尝试拆页，可用 `--large-lines` / `--large-symbols` 调整。依据限定符中的声明归属和名称主题词分组，不机械切行；保留每组符号和原始行范围。单个巨型符号或缺少分组依据时明确待人工拆分，不虚构职责。索引符号分配数量不代表行为完整性已验证。
- **复杂功能候选**：复用已有 CodeWiki 的调用、路由、导入、继承、实现和配置关系，从入口沿有向依赖查找跨文件代码；入口不明显的循环也作为候选。每个候选保留符号、边和到文件页的链接，文件页也能返回相关功能。
- 功能候选优先入口端点及 main/run/start/execute/handle 类入口，默认展开 80 个（`--feature-limit` 可调整）。其他候选入口记录在 `deferred_feature_entries`，页面显示数量和清单，不静默当作已覆盖。每个候选调查最多 4 跳、80 符号或 160 条边，截断时显式提示。
- 静态解析链仍可能包含推断，尤其全局同名符号匹配。职责主题词不证明真实职责、依赖可达性不证明业务边界；所有拆页和功能均为 `proposed/review_required`。本阶段不命名或生成未经源码语义核实的业务说明。

规划要求提交匹配、分析完成且文件哈希校验通过的现有图。没有图时拒绝规划，不自动建立索引或调用模型。文件行数直接来自冻结 Git blob；页面搜索、目录展开、双视角跳转和 JSON 阅读均为只读。schema 3 规划与旧 schema 1/2 影响预览分别展示。

## 验证

### zgame 规划实例与正文生成要求

已对 `zgame/master` 的冻结提交 `9052b9e77e681315a200a8489dfc6938d81024db` 运行独立规划，快照为 `39de705b8385fa00e1738069f8655cb1c9287cb9a77b1552192d3bf29a3ceace`。在 <http://127.0.0.1:5490/planning#39de705b8385fa00e1738069f8655cb1c9287cb9a77b1552192d3bf29a3ceace> 查看。

该轮使用 `.ocr-data/wiki-zgame-plan/zgame` 的独立检出，静态图为同目录旁的 `graph.sqlite3`，只写独立图与规划状态。检出通过本地对象 alternates 引用托管 bare 库以避免复制整个历史，不修改源仓库；如以后重建图，仍依赖该本地对象库可用。已经保存的规划快照阅读不依赖源检出。

预览记录 `generation_policy`：目标服务 AI Mana、当前审查模型 `glm-5.3`、审查设置版本 4、`assistant_authorship=false`、`generation_enabled=false`。这是用户指定的后续正文生成要求，本轮未调用、未验证正文生成接口，也未把该要求冒称为已运行的模型连接。未来正文必须经 Wiki 工具调用该服务模型生成，助手只负责接入、运行和验证，不得撰写、补写或改写 Wiki 正文。

本次模型调用 0，独立库的 `llm_run/doc_page/doc_catalog` 均为 0。2 项解析失败和未入图项保留在快照中，不能宣称已完整理解所有源码。初始 Wiki 和审查流程保持不变。运行摘要见 `.ocr-data/wiki-zgame-plan/validation.json`。

### 模块回归检查

```powershell
python scripts/checks/run-tests.py --suite wiki
```

测试覆盖队列去重、合并、竞争领取、租约恢复、乱序完成、失败重试、分支与配置隔离、不可变快照、篡改检测、引用基线、重命名、中文文件路径及禁网执行；另覆盖依赖传播方向、多跳/循环、两侧删除/重命名、置信度择路、跨范围传播、图冻结及缺图/错版/未完成/损坏/覆盖不足降级。测试使用临时 Git 仓库，不操作审查服务。
