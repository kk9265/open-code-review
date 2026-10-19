# Wiki 预览模块

本模块独立于审查服务，只提供本地 Git 差异预览。没有模型调用、Wiki 正文生成、Wiki 发布或自动启动入口。运行时仅依赖 Python 标准库和本地 Git，不读取工作台模型设置或审查数据库。

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

## 影响精度

预览包含增删改与重命名、直接引用受影响的页面、父页面和新增目录的检查建议。没有调用 CodeWiki 分析器或模型。

间接依赖尚未分析，因此存在代码变化时，其他页面放入 `potentially_affected_pages`，不能当作已验证可复用页面。缺少清单时返回 `catalog_required=true`，不虚构页面。token 数保持未知，实际计划调用数始终为 0。目录建议需要后续校核，不会自动创建页面。

## 验证

```powershell
python -m unittest discover -s integrations/wiki-preview -p "test_*.py" -v
```

测试覆盖队列去重、合并、竞争领取、租约恢复、乱序完成、失败重试、分支与配置隔离、不可变快照、篡改检测、引用基线、重命名、中文文件路径及禁网执行。测试使用临时 Git 仓库，不操作审查服务。
