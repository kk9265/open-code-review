# Wiki 工作区交接

更新：2026-10-19。LoginServer 剩余模块已由 Wiki 模块调用 AI Mana DS 完成生成、检查和发布。用户随后否定将全部函数说明合并到一篇“函数索引”的实现；现已改成“业务过程 / 类与函数”两个导航入口。当前 10 个业务页、28 个轻量类或源文件目录、191 个独立函数页，共 229 个阅读文件。AccountServer 仍为空。本次只迁移结构，未调用模型或改写说明文字。

## 必须遵守的边界

- 助手不得自行撰写、翻译、补写或改写任何 Wiki 正文。正文必须由 Wiki 模块调用 AI Mana 模型生成；不能手写“示例正文”冒充生成结果。
- 最新授权允许维护扩展执行入口并运行 LoginServer 剩余模块；此前“仅一个试点、等待确认后才能扩展”的限制已被用户本轮确认更新。AccountServer 不在扩展范围，继续保持为空。
- `LoginWhiteList` 是已认可的试点。LoginServer 其余模块的语义归组、补充源码选择、正文、源码对照检查和正文修正均由 Wiki 模块调用 AI Mana 的 `deepseek-flash` 完成。助手只能维护执行机制和发出命令，绝不能代替 DS 撰写或补写正文。其他服务仅作本轮调用链上下文，不生成其独立 Wiki。
- 不得将“函数列表独立”解释成“所有函数说明合并一篇文档”。最新要求采用独立的“类与函数”导航：按冻结源码中的限定归属组织，自由函数按源文件归组，每个函数独立阅读。业务页不重复函数列表。函数说明必须原文保留；装配器只调整标题层级、目录与结构性源码返回链接。
- 修改提示词、上下文构建、校验和阅读界面属于模块维护；正文错误或过长时交回模块调用模型处理，不能由助手编辑正文。
- 不创建 PR，不暂存、不提交、不清理既有代码改动。测试仅在本地保留。
- 源码保持英文、SPDX、LF；中文 UI 放 i18n 并使用 Unicode 转义。禁止默认编码 PowerShell 重写文件。

## 当前可查看的结果

工作台：<http://127.0.0.1:5484/wiki?scope=LoginServer>。

试点正文：<http://127.0.0.1:5484/wiki/scopes/LoginServer/read/page/class-b32ed152dddbea81c404>。

- LoginServer：10 个业务页，白名单原有 5 段过程保留；191 个函数按 28 个类或源文件分组，单函数单页，类目录只列签名、现有模型摘要及链接。没有全局函数正文长页，业务页无函数列表。
- AccountServer：0 页，保持为空。
- 页面没有流程图、“证据”与“源码依据”；提供概述、业务过程、函数索引、函数说明和源码跳转。
- 函数说明包含作用、文字流程和有意义的边界处理；其他语义按需展示。简短函数不展示额外语义栏目。
- 导航提供“业务过程 / 类与函数”两个入口，类目录可折叠并显示数量，搜索匹配函数签名和已有模型摘要；重载显示签名以区分。支持稳定锚点、源码跳转与返回原业务段落或函数位置。新增业务单页不把同名标题重复作为子目录。
- 发布清单 `mode=function-guide`，`review_status=awaiting_user_review`。机器检查通过不等于用户已认可可读性。
- 当前发布目录：`.ocr-data/wiki-loginserver-generated/releases/51049f2f785f3d2f200aab46fffe86204870207f91c25237fdb3a3acc9a01041/markdown`。此前 11 页发布仅作历史记录，不要恢复全局函数正文长页。
- 本轮运行：`scope-runs/5ac0d7abd244a6a358857398f53f6b1395fd6cf5918262a2dae4c60510e35b1b`。`acceptance.json` 保存覆盖与模型检查结果；`model-audit.jsonl`、精确请求、源码上下文和检查点保留调用来源。`scope_generation` 记录 17 个非脚本文件和剩余 184 个函数。
- 原试点运行 `pilot-runs/e138a7afc3ee9d61e405a9d63743e636f76a6e8d7f66ce59e80651c332d8569a` 保留历史检查记录。白名单业务原文未改；原七个函数的说明已从历史索引原文迁到各自函数页，仅修改标题层级和结构性源码返回地址。
- 当前结构验证：`.local-tests/wiki-function-layout-validation.json`；逐一核对 191 份函数文字和全部业务页原样保留、229 页哈希、28 个分组、22 组源码返回和 AccountServer 空状态。截图：`.local-tests/wiki-function-layout.png`。`.local-tests/wiki-scope-validation.json` 和旧截图仅记录此前 11 页布局。

Review、Wiki 阅读与旧目录预览仍在同一个 Go 进程、5484 端口。新二进制已安全部署，检查时 PID 为 **208260**；未来操作必须重新确认 PID，不能照抄。

## 已清空的内容

首次清空时，两个 `wiki-*-generated` 目录中的旧 manifest、Markdown、发布版本/历史、正文 SQLite、翻译中间结果、旧审计与截图均已删除；随后先发布试点，再完成本轮扩展。当前版本、历史与检查点均为清空后的新产物，不能与清空前的旧文件页混淆。旧 AccountServer 修复前本地清单备份也已删除。

保留模型选择文件 `generation-model.json`、本地工具源码、共享冻结源码、代码图和只读规划快照。旧规划不是当前函数指南的归组输入。初始保护数据 `.ocr-data/wiki-validation/baseline/` 不属于当前已发布 Wiki，未修改，也不得拿来恢复旧内容。

旧“LoginServer 114 页、AccountServer 53 页/51 页中文”的交接状态已失效，禁止按旧数字补生成或恢复。

## 实现路径

- `generate.py`：统一命令入口。新增 `--class-name / --header / --implementation`；普通整批文件页生成默认已禁用。
- `generate.py --scope LoginServer --remaining-modules`：本轮新增的显式扩展入口。只接受 LoginServer 和 AI Mana DeepSeek；不能混入旧文件页模式。`--dry-run` 无模型调用，报告剩余 184 个函数定义、17 个非脚本文件和解析不确定性。启动/停止脚本沿用排除规则。
- `scope_context.py`：自动抽取完整冻结函数、声明文件和依赖可用性，按 DS 的结构化请求读取跨类函数/协议源码；仅精确符号匹配，不按短名称猜调用边。解析不确定的函数附完整源文件，超预算显式记录而不截断。
- `scope_guide.py`、`prompts/scope-guide.md`：DS 负责业务模块归组；模块校验每个函数恰好分配一次且所有非脚本文件有归属。DS 决定需要补读的源码，生成业务说明及成批函数说明，再由 DS 检查和有限修正。最终交给 `function_pages.py` 按源码归属拆成独立函数页，业务页无函数列表。
- `function_pages.py`：纯结构迁移，不调用模型、不生成解释；从精确冻结源码范围确认类归属，自由函数按原文件归组。按函数身份保留重载、锚点、原文及源码返回。默认只预检查，`--scope LoginServer --publish` 原子发布，已经拆分则不重复操作。
- `generate_scope.py`：固定 CodeWiki 网关、本机 AI Mana、仅 `deepseek-flash`；精确请求缓存、模型审计、进度、发布前覆盖检查与原子切换。归组/函数格式输出使用非思考模式，源码检查和其他步骤使用低推理；不能拿推理内容充当正文。
- DS 最初归为 7 组，其中一组因职责较宽由 DS 再拆为 3 个过程，最终为 9 个新增业务页；函数仍逐个唯一归属，不增加源码范围。格式失败可缩小批次、对单个函数使用更精简的输出结构；文字只由 DS 重生成。模型意见累计传回 DS，禁止助手代修正文。
- 精确符号请求允许参数拼写不同的完整限定名：提供该限定名的全部真实重载并明确标记签名未匹配，不猜选一个；可以按精确类名补读完整类定义。短名称仍不推断调用边。
- `cpp_context.py`：从固定 Git 提交读取源码，以 Tree-sitter 识别类与函数，匹配声明/实现、重载和限定符；提供完整函数体、类声明、直接类内依赖及可取得的直接 include。缺实现、解析错误、歧义或超预算时拒绝生成，不静默截断。
- `prompts/function-guide.md`：类概述、函数说明和源码对照检查的模型约定。
- `function_guide.py`：结构、中文、长度、函数身份检查；只装配模型返回的说明与 UI 标题、代码索引、代码链接，不补写解释性正文。
- 旧 `business_context.py` 已删除：固定五组业务、函数路径、支持文件和宏选择逻辑不再保留。共用 `parse_functions` 移入 `cpp_context.py`，当前范围生成和函数页面归属识别均使用它。业务归组与补读源码继续由 DeepSeek 决定。已有正文与检查点不变。
- `business_guide.py`、`prompts/business-guide.md`：由 Wiki 网关生成调用/数据关系、主要过程、最终结果和必要缺口，再由模型对照源码审阅并有限修正。缺源码必须引用已验证缺失的依赖 ID；运行时边界与本次未选入的可用源码分别标记。无流程图、无证据栏目，装配器不写解释。
- `generate_functions.py`：调用固定版 CodeWiki 的 `LLMGateway`，只连接本机 AI Mana。逐函数生成后再让模型对照源码检查，必要时由模型有限修正；全部通过才发布。当前明确限制为 LoginServer / LoginWhiteList。
- 结果缓存绑定请求上下文与提示词；兼容缓存仅复用同一源码上下文、同一模型且通过当前校验的模型结果。不会使用清空前的旧正文数据库。
- `publish.py`：内容哈希、不可变版本和原子清单切换。
- `atomic_json` 对 Windows 临时共享占用作有界重试；持续拒绝访问时保留原文件并报错，不删除原文件绕过。已有发布不完整覆盖。
- `reset_generated.py --discard-all-generated`：显式丢弃两个服务的生成输出；检查目标路径与链接，运行中拒绝清空。破坏性命令，不能在接手或验证时自动执行。
- `internal/platform/wiki.go`、`integrations/wiki-preview/ui/wiki-reader.*`、`internal/platform/ui/static/wiki.js`：新导航、函数锚点、源码返回、空状态，以及旧阅读位置失效后的恢复。

## 上下文与篇幅

固定源码：`.ocr-data/wiki-zgame-plan/zgame`，提交 `9052b9e77e681315a200a8489dfc6938d81024db`。模型仍取 LoginServer 的 `generation-model.json`，当前 `deepseek-flash`，Review 设置独立。

试点中 `util_thread.h`、`util_mysql.h` 等库实现不可用，必须保留不确定性。`__TRY__ / __CATCH__` 仅在解析边界时用等长空白处理，给模型的源码始终保留原文；不得推断缺失的宏展开。

说明长度由函数规模控制，简短函数的步骤、边界与额外语义更少。整体说明文字预算不计算行内代码标识符，避免 C++ 长名称挤掉必要说明；每段长度和项目数量仍受约束。

模型生成过一版较长试点，随后收紧结构并由模块重生成，助手没有改写正文。当前发布的是精简版；历史试点与未发布缓存不是用户已认可的结果。

本轮业务过程也全部由模块调用模型生成。过程中有空响应和源码检查未通过的结果，均未发布；校验通过的精确请求检查点可复用。业务修正最多两轮，第二轮带累计问题和上轮文本，减少修复一处后遗漏已有边界；最终五段源码检查全部通过。`deepseek-flash` 业务请求显式使用 `reasoning_effort=low`，响应额度保留推理与 JSON 空间，空正文仍判失败。

已补充的源码检查点包括游戏服无角色时只设过期时间、角色简单数据缺失/标志未变时提前返回、资料缺失标志导致缓存替换，以及区服列表先转换响应后才修改局部列表的实际顺序。这些解释均由模型生成或修正，助手未补写。框架路由、异步送达和 Redis 实际写入不能由静态调用证明；相关正文保留边界。缺失依赖与可用但未选入的源码分开处理。

## 仅在后续明确授权时使用的试点命令

本轮扩展已明确授权，执行命令如下；接手先检查现有进度、锁与进程，避免重复启动。

```powershell
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --remaining-modules --dry-run
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --remaining-modules --concurrency 3
```

以下是旧单类试点命令，仅用于明确指定试点的维护；不要用旧试点命令覆盖扩展后的发布。旧 `--with-business` 入口已退役，传入即报错，不再执行硬编码业务选择。

```powershell
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --class-name LoginWhiteList --header LoginServer/LoginWhiteList.h --implementation LoginServer/LoginWhiteList.cpp --dry-run
.ocr-data/wiki-validation/venv/Scripts/python.exe integrations/wiki-preview/generate.py --scope LoginServer --class-name LoginWhiteList --header LoginServer/LoginWhiteList.h --implementation LoginServer/LoginWhiteList.cpp --concurrency 1
```

`--dry-run` 仅检查归组、完整实现及选中的业务上下文，不调用模型、不写正文。当前扩展已完成，不需重复生成；同一冻结提交再运行剩余模块命令会报告已完成。旧单类命令不能覆盖扩展发布，不得为了绕过保护清空其他页面；新提交或其他服务需要明确指令。

## 本轮验证与剩余事项

- Wiki Python 全套 91 项测试，覆盖原试点与预览回归，以及剩余范围分配、文件覆盖、DS 自动细分、批次缩小与精简函数、参数不同的限定名、类源码请求、跨进程缓存稳定、独立函数迁移原文保留和 Windows 原子替换重试。
- `go mod tidy`、格式化、`go test ./...`、`go vet ./...`、构建通过。
- 英文源码检查与 `git diff --check` 通过。
- 先前完成 11 页布局的覆盖验证；本次进一步逐份核对 191 个独立函数原文和全部业务页保留、229 页哈希及 22 组源码返回。源码覆盖仍为同一批 17 个非脚本文件；AccountServer 仍为 0 页。
- 浏览器实际验证业务导航、跨到 ProfileServer 源码并返回原业务段落，保存截图；此前函数导航验证仍保留。
- **本轮生成已完成。后续等待用户阅读反馈；正文错误或调整仍必须交回 Wiki 模块调用 AI Mana DS，助手不能补写或改写正文。不要接手自动重跑，不扩展 AccountServer。**
