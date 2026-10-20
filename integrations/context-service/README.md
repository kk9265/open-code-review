# 独立上下文服务

这个可选服务给网页审查提供 CodeGraph 查询和只读 Wiki 搜索。主程序不安装第三方包、不维护图数据库、不读写 Wiki。停用服务后，原有固定提交源码查阅仍可工作。

## 目录与启动

- `integrations/context-service/`：独立实现及配置示例，不进入 Go 核心依赖。
- `scripts/context/manage.py`：维护入口，Python 3.9 及以上，无额外 Python 依赖。
- `.ocr-context/vendor/`：本机 CodeGraph 可执行包，已被 Git 忽略。
- `.ocr-context/config.json`：本机配置，参考 `config.example.json`。
- `.ocr-context/data/repositories/<ID>/checkout/`：专用代码副本及 `.codegraph/` 索引。
- `.ocr-context/data/wiki/<ID>/<分支哈希>/`：独立业务知识。

本机配置使用 CodeGraph 1.6.2 的独立 Node 运行时及 CLI，未运行全局 `codegraph install`。升级工具前应验证适配接口和索引元数据兼容性。`codegraph_command` 是进程参数数组；Windows 推荐直接指定 `node.exe` 和 CLI JavaScript 的绝对路径，避免 `.cmd` 的 shell 参数解释。

```powershell
python scripts/context/manage.py --config .ocr-context/config.json serve
```

默认仅监听 `127.0.0.1:5487`。在工作台“设置 → 外部上下文”填入 `http://127.0.0.1:5487`；留空停用。配置中的 `source` 必须指向已登记的本地仓库，而不是网络地址。配置文件在启动时读取，修改后重启服务。

日常通过 `python scripts/workbench/start-web.py` 启动工作台时，只要上述地址已在设置中启用，启动器会自动启动本地上下文服务，并在工作台退出时关闭由本次启动的服务。若该地址已有兼容服务，启动器会复用它。仅在单独维护或排障时才需要手动运行 `serve`；可用 `--no-context` 临时禁止自动启动。

首次大仓库索引建议预热，避免占用审查时间：

```powershell
python scripts/context/manage.py --config .ocr-context/config.json index --repository 1 --revision <完整提交SHA>
```

后续打开审查会将副本切换到该轮 SHA 并执行 `codegraph sync`。准备超过网页连接时限时，本轮回退到内置工具，并记录缺失的上下文能力；可以先用上面的维护命令完成索引。服务不读取工作台数据库，也不会创建审查或调用模型。

首次构建时限独立配置为 `index_timeout_seconds`（默认 1800 秒），不属于模型审查预算。中断的索引保持未完成状态，下次维护会全量重建；只有完整索引才能作为增量更新基线。

## 版本与权限边界

`/v1/open` 接收仓库 ID、分支、固定 SHA，返回会话 ID、可用能力和说明。它是程序调用的准备接口，不是模型工具。

模型仅获得 `context_code(query)` 和有已批准文章时的 `context_wiki(query)`。`/v1/query` 不接受路径、命令或工具名称，只接受会话内的 `code/wiki` 能力；没有 Wiki 写入、索引修改或任意命令接口。

每仓库的跨进程文件锁覆盖索引准备和整个审查会话。正在审查时，维护命令无法更新同一个索引；关闭会话后才可切换版本。失联会话 20 分钟过期，活动查询不会被中途释放。不同仓库可独立使用。

CodeGraph 索引状态必须为 `complete`、`indexed_at_commit` 与审查一致，且没有已索引的未提交修改。查询前验证工作区版本和已跟踪文件状态。该状态表示索引器完成了其覆盖范围，不保证所有语言结构、宏、生成代码和动态调用均正确识别；搜索无结果不代表代码不存在。

副本由服务独占管理，不在其中手工编辑。模型查旧版本源码仍使用主程序原有的 SHA 读取工具；CodeGraph 当前会话只提供本轮最终上下文 SHA。

这是进程、数据和接口隔离，不是第三方程序的 OS 安全沙箱。服务以本机用户权限运行；不应直接暴露到公网。请求拒绝浏览器 Origin，不启用 CORS；CLI 子进程禁用遥测、自动更新检查和后台监听。索引与源码仍会按模型查询结果进入已配置的模型服务。

## Wiki 录入

获得某仓库分支的目录：

```powershell
python scripts/context/manage.py --config .ocr-context/config.json wiki-path --repository 1 --branch master
```

在返回目录中放置 `reward.md` 和同名 `reward.json`。元数据示例：

```json
{
  "approved": true,
  "branch": "master",
  "source": "需求单URL或人工确认记录",
  "applicability": "适用区服、版本或提交范围；需要模型核对",
  "updated_at": "2026-10-15"
}
```

只有明确批准、分支匹配且有来源的 Markdown 才进入查询。服务打开会话时冻结文章正文和 SHA256，后续文件修改不影响正在运行的审查；版本适用性是参考说明，服务不会自动推断业务规则适用范围。单篇最多 256 KiB、每分支快照最多 8 MiB。当前是关键词检索，返回最多 5 篇匹配文章；没有匹配不代表业务规则不存在。

Wiki 当前没有自动生成内容，也没有将 AI 报告写回知识库的流程。主程序会把返回文字视作证据，不能覆盖审查指令。

## 协议

所有接口为 JSON POST，调用方只需实现这三个接口即可替换服务：

- `/v1/open`：`{repository_id, revision, branch}` → `{session_id, revision, capabilities, notice}`
- `/v1/query`：`{session_id, capability, query}` → `{revision, text}`
- `/v1/close`：`{session_id}` → `{closed: true}`

主程序拒绝不同版本响应、非回环地址、重定向及超大响应；上下文查询及可用性记录随本轮审查保存。模型 API 的每次请求耗时写入工作台日志。
