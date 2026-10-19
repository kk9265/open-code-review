# AI Wiki 接入交接

交接日期：2026-10-18。仓库：`E:\code_review\open-code-review`，Windows PowerShell，当前分支 `main`。

用户正在切换模型。本次交接只保存状态，不意味着授权下一阶段开发。上一轮要求的四项工作已完成：固化 CodeWiki 补丁和依赖、独立任务队列、版本隔离、只预览模式。实现是独立 CLI，不是已经接入工作台的 Wiki 网站。

## 必须保持的约束

- **不得调用模型，不得生成或更新 Wiki 正文、目录。** 用户明确担心生成成本失控。未来只有用户明确重新授权才可做真实生成。
- **不得改动现有审查流程。** 不接入审查队列、审查数据库、审查 worker、工作台路由或模型设置。
- 不运行旧验证脚本 `run_probe.py` 的生成命令，不运行上游 `wiki`、`watch` 或默认会生成内容的 `update` 命令。
- 不为验证而运行 `ocr review`，它可能调用模型。本轮没有提交或创建 PR，因此没有触发提交前审查要求。
- 不清理、重置、暂存或覆盖用户已有的未提交改动。根目录 `HANDOFF.md` 在开始前已经被删除，保留这个删除状态。
- 遵守根目录 AGENTS.md：Windows Go 命令用 `scripts/toolchain/go.ps1`；源码英文、SPDX、LF；中文说明放 Markdown。不要用默认编码的 PowerShell 文件重写命令。

## 当前实现

新增代码全部在 `integrations/wiki-preview/`，当前为未跟踪、未提交文件。没有修改现有生产代码。

| 文件 | 职责 |
| --- | --- |
| `worker.py` | CLI、固定入队参数、验证版本指纹、单次执行预览任务 |
| `store.py` | 独立 SQLite 队列、去重、合并、领取租约、快照和当前预览指针 |
| `git_data.py` | 本地 Git 提交、树、差异、范围及已有页面引用清单读取 |
| `planner.py` | 只预览影响计划，无 CodeWiki 分析器或模型调用 |
| `prepare.py` | 从本地克隆复现固定上游提交并应用补丁，校验源码和可选依赖版本 |
| `vendor-lock.json` | 上游提交、补丁和依赖清单哈希、修复后源码文件哈希 |
| `requirements-windows-py312.lock` | 已验证的 Windows Python 图分析依赖版本 |
| `patches/0001-incremental-graph.patch` | CodeWiki 增量代码图修复及其回归测试 |
| `CODEWIKI-LICENSE.txt` | 上游 MIT 许可证 |
| `test_preview.py` | 21 项独立预览模块测试 |
| `USAGE.md` | 操作说明、队列规则、输入格式和限制 |

预览模块自身只依赖 Python 标准库和本地 Git，不需要安装或导入 CodeWiki、LiteLLM、OpenAI SDK，不读取模型凭证。`prepare.py` 不安装依赖，也不联网克隆；依赖清单用于固定和校验之前验证过的图分析环境。

### 队列和版本语义

- 默认状态目录：`.ocr-data/wiki-preview/`；独立数据库：`wiki-preview.sqlite3`。
- 三张独立表：`jobs`、`snapshots`、`current_previews`。
- 仓库身份由本地规范化绝对路径决定，同远程不同克隆目前视为不同仓库。
- 入队时固定分支目标提交、显式基线、范围和页面引用清单；分支前进或清单后来被修改，不改变已入队任务。
- 同输入及实现版本去重；同分支连续请求合并尚未执行的旧任务，旧记录为 `superseded`。
- 领取使用 SQLite 写事务和 300 秒租约；过期后下次 `work-once` 可重新领取；旧领取令牌不能完成新领取者的任务。
- 较早任务完成不能覆盖更新请求的当前预览。失败保留已存在的当前预览。
- 快照身份包含仓库、分支、提交和配置。文件路径为 `snapshots/<哈希前两位>/<完整哈希>.json`，避免 Windows 长路径问题。
- 快照文件先写临时文件再替换，数据库事务登记快照及当前指针；已存在内容不能被不同内容覆盖，读取时检查 SHA256。
- 发布的是**预览快照**，不是 Wiki。快照保存 Git 树路径及对象标识、差异和计划，不保存生成正文，也不复制完整源码。
- 预览实现版本变化时，旧排队任务会失败并要求重新入队，不会用新实现悄悄产出旧配置身份的结果。

### 预览精度和未实现部分

影响分析目前仅使用 Git 差异、已有页面的源码引用和页面父子关系。直接受影响页面给出原因，新增或重命名路径给出目录检查建议；未能确认的间接影响列在 `potentially_affected_pages`，不能认定为安全复用。

缺少引用清单时返回 `catalog_required=true`，不虚构目录或页面。清单提交必须等于比较基线。token 估算为未知，计划模型调用数始终是 0。

当前没有：工作台 UI、后台常驻轮询、Webhook、自动接入审查、真实生成、Wiki 发布或代码图依赖影响分析。没有把旧的 CodeWiki 原生 `watch` 当作自动 Wiki 更新器。

## 已完成的验证

### 当前独立预览模块

- 21 项测试通过：并发去重、任务合并、竞争领取、过期租约隔离、过期旧任务不复活、乱序完成、失败重试、分支与配置隔离、快照完整性、孤立快照恢复、入队参数冻结、分支前进、基线校验、重命名、中文文件名、禁网运行，以及生成子命令不可用。
- Ruff 检查通过。
- `scripts/toolchain/go.ps1 run scripts/checks/verify-english-only.go` 通过，扫描 486 个源码文件。
- 新增源码均为 LF，带 SPDX；`git diff --check` 通过。没有运行 Go 全套测试，当前没有修改 Go 源码。
- 固定补丁已在全新本地克隆上成功应用并校验；原验证 Python 环境的依赖版本校验通过。
- 真实仓库预览：`1fc587e408dca15e4d4e31cc700df7a209670b19` → `8aae142ff6b10320cb5a51076a4ad74942c8f986`，识别 54 个变化文件，27 个直接受影响页面。这里只读取了历史页面引用，没有生成页面。
- 两版预览均可读取，重复请求返回同一任务，下一次执行返回 `idle`。
- **本轮模型调用 0，Wiki 生成 0。**

验证产物位于 `.ocr-data/wiki-preview-validation/`：

- `validation.json`：汇总结果。
- `preview-example.json`：真实提交预览样例。
- `state/wiki-preview.sqlite3`：独立验证队列，有 2 个成功任务。
- `state/snapshots/`：两版不可变预览。
- `pinned-codewiki/`：用于复现补丁的独立本地克隆。

上述状态目录是验证用目录，不是默认运行目录。没有启动后台服务。

### 之前的增量图修复

上游固定为 `PorunC/CodeWiki`，提交 `7be8f702504cbd69aec6491a2e4e81f5311e0ba6`，版本 0.6.5。原始增量实现会丢失图关系，并反复误报配置文件新增。

补丁在文件节点中保存完整 AST 符号快照，恢复符号顺序及所有解析字段；配置节点参与哈希比较；没有快照的旧索引自动重新解析一次，随后无变化返回零解析；内部解析缓存排除于 GraphRAG 模型上下文。

此前修复验证：31 项测试通过，7 个源码文件类型检查通过。真实提交增量只解析 23 个文件，图结果与全量分析逐项相等：5,679 个节点、35,103 条边。单文件修改只解析 1 个文件，无变化解析 0 个文件。该轮也是零模型调用、零 Wiki 生成。

原始修复和日志仍在 `.ocr-data/wiki-validation/`，包括 `graph-fix-results.json`、`graph-fix-checks.json` 和修改后的 `vendor/CodeWiki/`。当前可复用补丁已复制进项目，不再只存在于忽略目录。

## 安全的复测命令

在 `E:\code_review\open-code-review` 执行：

```powershell
$env:LC_ALL='C'
python -m unittest discover -s integrations/wiki-preview -p 'test_*.py' -v
.ocr-data/wiki-validation/venv/Scripts/ruff.exe check integrations/wiki-preview
./scripts/toolchain/go.ps1 run scripts/checks/verify-english-only.go
```

只读查看已有预览：

```powershell
python integrations/wiki-preview/worker.py --state .ocr-data/wiki-preview-validation/state current
python integrations/wiki-preview/worker.py --state .ocr-data/wiki-preview-validation/state snapshots
```

真实比较的入队命令如下。只有用户需要继续验证时再运行，不必为接手而重复：

```powershell
python integrations/wiki-preview/worker.py --state .ocr-data/wiki-preview-validation/state enqueue --repo . --branch main --commit 8aae142ff6b10320cb5a51076a4ad74942c8f986 --baseline 1fc587e408dca15e4d4e31cc700df7a209670b19 --catalog .ocr-data/wiki-validation/baseline/manifest.json
python integrations/wiki-preview/worker.py --state .ocr-data/wiki-preview-validation/state work-once
```

`--baseline` 不会自动选择上一个预览提交。使用不同实现版本会改变任务身份；不要把这种情况当作去重失效。

## 工作区保护和后续接手

工作区本来就有大量审查功能改动，包括 `go.mod`、`internal/platform/`、`internal/agent/`、`internal/llmloop/`、`internal/session/`、`internal/tool/` 以及迁移 009 至 013 等新文件；这些不是本次 Wiki 工作的修改。根目录 `HANDOFF.md` 和 `internal/platform/autonomous_review.go` 也原本处于删除状态。

先读本文件、`USAGE.md` 和 `git status --short`。不要执行全仓库恢复、格式化、暂存、提交或清理来“整理”这些用户改动。当前 Wiki 模块和本交接文件尚未提交。

用户此次仅请求交接。下一轮应承接其新指令，不自行开始真实生成、UI 接入或后台任务。若用户要求继续改进预览，可以优先讨论代码图间接影响分析，但仍须保持零模型调用，并继续与审查流程隔离。
