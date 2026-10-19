# 本地仓库维护工具

统一入口为 `scripts/repositories/manage.py`，支持 Python 3.9+，适用于 Windows 和 Linux。
脚本直接维护 `.ocr-data/workbench.db` 和 `.ocr-data/repositories/<仓库ID>.git`，
不调用 Web 接口，不创建重复记录，也不分析代码。工具入口统一使用 Python。

## 使用方法

在项目根目录执行（Linux 可将 `python` 换为 `python3`）：

```bash
# 查看仓库 ID、名称、状态和缓存路径。
python scripts/repositories/manage.py list

# 首次拉取或更新 zgame，并更新数据库状态。
python scripts/repositories/manage.py fetch kk9265/zgame --pause-workbench

# 查看本地缓存中的分支和提交 SHA，不读取源码。
python scripts/repositories/manage.py branches kk9265/zgame --pause-workbench
```

支持仓库 ID、精确名称或 owner/name；有重名时使用 ID。
`--data-dir` 指定已有数据目录，默认按脚本所在项目定位，不依赖当前终端目录。

`--pause-workbench` 使用 psutil；需要时安装：

```bash
python -m pip install -r scripts/repositories/requirements.txt
```

不使用暂停恢复功能时不依赖 psutil，先停止工作台，再省略该参数即可。
查看列表可与 Web 同时运行。缓存维护使用数据目录锁，避免并发写入。
暂停选项会核对程序路径、数据目录和同步状态，保留原启动参数、目录和环境，
完成或失败后在后台恢复工作台。存在运行中或排队中的同步时拒绝暂停。
维护期间 Web 暂时不可用，强制关闭终端可能导致无法自动恢复。

## 工具链和凭据

脚本用项目隔离的 Go 1.26.9 自动构建 `cmd/ocr-repo`，产物为
`dist/ocr-repo.exe`（Windows）或 `dist/ocr-repo`（Linux）。
不会修改系统 Go 设置，也不通过 PowerShell 执行维护流程。
Go 负责 Git 同步和数据库一致性，Python 负责命令行入口、凭据和进程管理。

Windows 优先使用进程环境变量 `OCR_GITEA_TOKEN`，否则通过 Windows DPAPI
读取当前用户已保存的 `gitea-token.dpapi`，无需重新输入。
首次配置仍可使用现有 `scripts/workbench/start-web.py --gitea`。
Linux 使用进程环境变量 `OCR_GITEA_TOKEN`。
令牌不进入命令参数或 Git URL。

## 拉取行为

### 本机 Gitea：本地源模式

Gitea 和工作台在同一台电脑上时，可直接读取源 bare 仓库，不经过 HTTP，
也不需要令牌或运行中的 Gitea 服务：

```powershell
python scripts/repositories/manage.py fetch kk9265/zgame --local-source "C:\gitea\data\gitea-repositories\kk9265\zgame.git" --pause-workbench
python scripts/repositories/manage.py fetch kk9265/zgame_china --local-source "C:\gitea\data\gitea-repositories\kk9265\zgame_china.git" --pause-workbench
```

`--local-source` 仅用于 `fetch`，指定实际 bare 仓库路径；Linux 使用对应绝对路径。
源仓库只读，目标仍是工作台自身的 `<仓库ID>.git`，原仓库网页地址保持不变。
首次同步使用本地 clone 复制对象，不使用硬链接或共享对象目录，然后同步分支和标签；
已有缓存使用本地增量 fetch。完成后回写同一份数据库同步状态。
本地复制期间 Git 不提供逐字节进度；传输性能取决于对象数量、磁盘及实时扫描。
源库不能与缓存目录重叠，不支持使用 objects/info/alternates 的源库。
源库在复制时若发生清理或改写导致失败，应在源库稳定后重试。
该参数只影响本次命令。若希望永久使用本地同步，在仓库详情的“分支跟踪”中保存本地同步方式及源路径。之后 Web 拉取和未指定 `--local-source` 的维护命令都会沿用保存的方式，本地方式不需要令牌。

### HTTP 模式

`fetch` 拉取全部分支和标签，显示实时 Git 进度，不设 15 分钟总时限；
仍保留 60 秒低速连接保护。首次失败重试不保证字节级断点续传。
已完成的缓存可增量更新，失败不会删除原有缓存。
这是 bare Git 缓存，不是日常编辑目录；不要用普通 checkout 覆盖它。

当前维护范围是已在工作台登记的 Gitea 仓库，不自动接管任意手工放入的目录。

启用 AI 审查队列后，使用 `--pause-workbench` 前请先在“审查”页暂停队列并等待当前轮次结束。脚本会拒绝中断已启用或正在运行的 AI 队列。维护完成恢复工作台后，可在页面重新启用队列。
底层 `ocr-repo` 也可独立使用，通过 `-data-dir`、`-action`、
`-repository` 参数执行相同操作；其 `list` 输出 JSON。
