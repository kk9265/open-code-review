# Wiki 工作区交接

更新：2026-10-19。工作目录：`E:\code_review\open-code-review`，Windows PowerShell，分支 `main`。

## 接手先读

用户此次只要求精简交接，准备切换当前对话模型；没有要求重新生成 Wiki 或更改审查模型。上一轮授权的入口与导航整理已完成，没有待执行的生成任务。

- Wiki 面向目标仓库的文件职责、复杂功能和大文件职责拆页；提交影响分析只是维护辅助。
- **助手不得撰写、补写或改写 Wiki 正文。** 正文只能由现有 CodeWiki 工具调用用户指定的 AI Mana 模型生成；助手负责接入、运行与机械验证。
- 阅读、导航、图表和引用样式可以继续维护，默认零模型调用；不要为验证运行 `ocr review`、旧生成探针、`wiki/watch/update` 或生成执行器。
- 不覆盖初始 Wiki，不改审查队列、审查执行流程或生产模型设置，不自行扩展到其他服。
- 工作区有大量既存暂存/未暂存改动，不重置、清理、整体格式化、暂存或提交。根目录 `HANDOFF.md` 和 `internal/platform/autonomous_review.go` 原本已删除，保留该状态。
- 遵守 AGENTS.md：Go 用 `scripts/toolchain/go.py`；源码英文、SPDX、LF；中文 UI 放 i18n。不要用默认编码的 PowerShell 命令重写源文件。

## 已交付状态

唯一用户入口：<http://127.0.0.1:5484/wiki>。Review 侧栏「Wiki」中切换「Wiki 正文／目录规划」，不再要求打开独立页面。

- 正文：文件／复杂功能双目录，职责子页挂接父文件；搜索、面包屑、当前页标记。
- 正文与规划各自恢复页面、版本、搜索及展开状态。规划切换仓库版本时顶部元信息同步更新。
- Mermaid 本机渲染，支持适应宽度、缩放、查看原始代码；异常时保留代码。
- 正文引用显示为小号上标，悬停显示源码位置；文末 Sources 默认折叠；点击定位并高亮源码行，可返回原正文。
- 规划目录过滤支持按相对目录排除及清空，只影响展示，按仓库在浏览器保存。
- 内部 5491 正文服务和 5490 规划服务经 Review 同源只读代理 `/wiki/read/*`、`/wiki/plan/*` 嵌入。固定本机目标，不转发 Cookie/Authorization，不提供生成入口。后台服务停止时返回 503，需要启动服务，不会自动生成。

## 当前 Wiki 与模型

- 目标：`zgame / master / LoginServer`，冻结提交 `9052b9e77e681315a200a8489dfc6938d81024db`；不是 open-code-review 功能自身的 Wiki。
- 已完成 **114/114 页**，全部通过 CodeWiki 工具校验，无失败项：17 文件页、88 职责子页、9 跨文件功能页。
- 来源为 112 篇 `glm-5.3` + 2 篇 `deepseek-flash`。后两篇补齐原先失败页，没有重写原 112 篇。
- AI Mana 显示名 **DeepSeek V4.1 Flash** 对应 ID **`deepseek-flash`**，不是 `deepseek-v4-flash`。名称映射已从本机 `%LOCALAPPDATA%\.aimana/client-runtime-config.json` 验证，真实调用返回 ID 也已核实。
- Wiki 执行选择持久化于 `.ocr-data/wiki-loginserver-generated/generation-model.json`，当前为 `aimana / deepseek-flash`。审查设置仍为 `aimana / glm-5.3`，两者没有联动修改。
- “工具校验通过”不代表语义完整性已人工确认；职责拆页和复杂功能边界仍需后续评估。此轮只做了界面整理，没有生成/修改正文。

## 关键源码

| 位置 | 用途 |
| --- | --- |
| `internal/platform/wiki.go`、`wiki_test.go` | Review Wiki 页及固定目标只读代理、回归测试 |
| `internal/platform/ui/wiki.html`、`ui/static/wiki.js/css` | 统一工作区、页签和阅读位置恢复 |
| `integrations/wiki-preview/generated_viewer.py` | 正式只读正文服务、文件树、面包屑、源码读取 |
| `integrations/wiki-preview/ui/wiki-reader.js/css` | 阅读状态、引用显示、源码行高亮 |
| `integrations/wiki-preview/ui/wiki-diagrams.js` | Mermaid 渲染与缩放 |
| `integrations/wiki-preview/viewer.py`、`ui/catalog.js` | 只读规划服务及双目录预览、过滤、状态恢复 |
| `catalog_preview.py`、`graph_data.py`、`graph_impact.py` | 冻结源码/代码图的规划与影响分析 |
| `prepare.py`、`vendor-lock.json`、`patches/` | 锁定 CodeWiki 与两份二次开发补丁 |
| `USAGE.md` | 完整运行、依赖与预览操作说明 |

CodeWiki 固定提交 `7be8f702504cbd69aec6491a2e4e81f5311e0ba6`，MIT；通过既有原生生成器、提示词、引用校验保存正文。不要手工修改锁定上游副本或生成正文来修正内容。

## 数据与复用位置

均位于项目 `.ocr-data/`，多为忽略文件，切换对话模型后继续复用，不重复构图或生成。

- `wiki-loginserver-generated/`：正文 `codewiki.sqlite3`、`manifest.json`、`markdown/*.md`、`progress.json`、逐页 `page-models.json` 与 `model-audit.jsonl`。manifest 保存正文哈希和逐页模型来源。
- `wiki-zgame-plan/zgame`：冻结源码检出；`wiki-zgame-plan/graph.sqlite3`：已建代码图。检出依赖本地托管 bare 库 `repositories/1.git` 的对象，不修改这些源码或引用。
- `wiki-code-plan/`：全库规划状态；zgame 快照 `39de705b8385fa00e1738069f8655cb1c9287cb9a77b1552192d3bf29a3ceace`。其中还保存 open-code-review 历史规划，不能混作游戏仓库结果。
- `wiki-loginserver-plan/`：LoginServer 范围规划及原生工具生成执行器 `run_generation.py`。**不要为接手而运行**；执行器会跳过已生成页面，改模型参数也不代表重生成全部。
- `wiki-code-plan-validation/pinned-release/`：已校验的 CodeWiki 副本；`wiki-validation/venv/Scripts/python.exe`：生成/图分析环境。
- `wiki-validation/baseline/`：受保护的初始 Wiki。正文输出中的 `protected-baseline.json`、`model-switch-baseline.json`、`reader-change-proof.json` 保存完整性核验依据。
- `wiki-mermaid/node_modules/mermaid/dist`：固定 `mermaid@11.15.0` 本机资源。正文读取依赖见 `requirements-reader.txt`。
- 最终界面截图：`wiki-loginserver-generated/unified-review-wiki.png`。

旧 `5488` 是提交影响预览，旧 `.ocr-data/wiki-loginserver-generated/serve.py` 已被正式阅读服务替代，均不是当前用户入口。

## 启动与验证

当前 Review 运行 `dist/ocr-web.exe`，监听 5484。Go 模板/静态资源嵌入二进制，修改后需构建并安全重启；独立 Python 静态 UI 改动刷新即可，Python 服务端代码改动需重启对应服务。

只读服务启动命令（仓库根目录）：

```powershell
python integrations/wiki-preview/generated_viewer.py --state .ocr-data/wiki-loginserver-generated --port 5491
python integrations/wiki-preview/viewer.py --state .ocr-data/wiki-code-plan --port 5490
```

已有服务不要重复启动。Review 重启前先确认构建成功、没有活动审查任务并备份程序；复用已有 DPAPI 凭据，不能输出明文或无凭据替换运行服务。详细启动与依赖见 USAGE.md。

上一轮已通过：Go 全量测试、全量 vet、Python 52 项测试、Ruff、JS 语法、英文源码及 diff 检查。浏览器已验证双目录、职责面包屑、版本/页面/搜索恢复、3 张 Mermaid、源码引用折叠和行范围回跳；114 篇正文、manifest、模型审计及初始 Wiki 哈希未变化。此次仅精简交接，不重复执行生成或全量测试。

## 后续工作边界

入口和导航两项已完成。尚未开始的建议是 LoginServer 内容质量评估（重复拆页、职责粒度、功能覆盖），之后再考虑增量更新和其他服。**等待用户下一条具体指令**；需要修正文时仍须经 CodeWiki 和指定 AI Mana 模型完成，助手不能代写。
