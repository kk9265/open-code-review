# 项目脚本

工具脚本统一使用 Python 3.12 或更新版本，支持 Windows 和 Linux。下面的命令从项目根目录执行。
先安装共享依赖：`python -m pip install -r scripts/requirements.txt`。

```text
scripts/
├── context/
│   └── manage.py                 # 独立上下文服务、CodeGraph 索引、Wiki 目录
├── repositories/
│   ├── manage.py                 # 仓库列表、拉取更新、分支查看
│   └── requirements.txt          # 仓库维护的 Python 依赖
├── workbench/
│   ├── clean-artifacts.py        # 预览或清理旧的 Windows 阶段性构建
│   └── start-web.py              # 工作台启动与令牌配置
├── toolchain/
│   └── go.py                     # 项目隔离 Go 工具链
├── checks/
│   ├── run-tests.py              # 本地 Python 测试统一入口
│   └── verify-english-only.py    # 英文源码检查
├── licenses/
│   └── license.py               # 检查或添加许可证头
├── tooling.py                   # 工具链、进程识别与 DPAPI 共用逻辑
├── requirements.txt            # 工具共用依赖
└── README.md
```

## 仓库维护

```bash
python scripts/repositories/manage.py list
python scripts/repositories/manage.py fetch kk9265/zgame --pause-workbench
python scripts/repositories/manage.py fetch kk9265/zgame --local-source "C:/gitea/data/gitea-repositories/kk9265/zgame.git" --pause-workbench
python scripts/repositories/manage.py branches kk9265/zgame_china --pause-workbench
python -m pip install -r scripts/repositories/requirements.txt
```

仓库工具按脚本位置定位项目和默认数据目录，不依赖终端当前目录。
详细参数、凭据及维护边界见 [仓库维护说明](../docs/repository-maintenance.md)。

## 工作台与工具链

```powershell
python scripts/workbench/start-web.py
python scripts/workbench/start-web.py --gitea --restart
python scripts/toolchain/go.py version
python scripts/toolchain/go.py build -o .\dist\ocr-web.exe .\cmd\ocr-web
```

## 清理旧构建

```powershell
python scripts/workbench/clean-artifacts.py
python scripts/workbench/clean-artifacts.py --apply
```

默认只显示待删除清单和容量；`--apply` 才执行删除。
仅处理本项目 `dist` 直属的 `ocr-web-*.exe`、`ocr-repo-*.exe` 阶段性构建及
`ocr-web.exe~`，不递归清理目录。正式程序 `ocr-web.exe`、`ocr-repo.exe`、
`opencodereview.exe`、Linux 构建和其他文件保留。每个工具默认额外保留最近两份
阶段性构建及最近一份 `before-*` 升级前备份，可用 `--keep-per-tool 3` 增加保留数量。
最近的判断依据是文件修改时间。

脚本跳过 Git 跟踪的文件和可识别的运行中程序，拒绝经过符号链接或 junction 的路径；
删除前再次检查文件是否变化、是否运行或已被 Git 跟踪。删除失败会停止后续清理并报告，
不会终止服务。Wiki 正文、数据库、初始 Wiki、源码检出、生成环境、依赖、缓存和日志
均不在清理范围内。`wiki-validation` 等带有 validation 的目录仍有运行依赖，不可整目录删除。

脚本回归测试只在临时样例仓库中执行：

```powershell
python scripts/checks/run-tests.py --suite tools
```

## 本地测试与 Git 排除

本项目按个人使用方式维护测试：测试代码只在本机保留，不提交到 Git/Gitea。

- Python 测试集中在 `.local-tests/python/tools/` 和 `.local-tests/python/wiki-preview/`，
  不再放在工具或应用源码旁。运行全部 Python 测试：`python scripts/checks/run-tests.py`。
- Go 的同包测试仍保留在原包目录，以便访问包内符号；`*_test.go` 和专用辅助文件
  `internal/session/testing.go` 已取消 Git 跟踪并被忽略，本机仍可用
  `python scripts/toolchain/go.py test ./...` 运行。
- `.gitignore` 同时排除 `.local-tests/`、`test_*.py`、`*_test.py` 和 `*_test.go`。
  不要使用 `git add -f` 将这些文件重新加入仓库。
- 克隆仓库不会带回这些本地测试，需另外备份 `.local-tests/` 及本地 Go 测试文件。
  Python 测试入口在测试缺失时明确报错，不会把“没有测试”当作通过。
- 取消跟踪会表现为 Git 暂存区中的删除，但磁盘文件仍保留；已有提交中的历史测试
  不会被这次整理抹除。

## 源码检查与许可证

```powershell
python scripts/checks/verify-english-only.py
```

```bash
python scripts/licenses/license.py check
python scripts/licenses/license.py add
```

新增工具继续使用 Python，放入对应用途目录。通用依赖放在 `scripts/requirements.txt`；
上下文服务等独立运行环境继续使用各自的依赖清单。

## 可选上下文服务

```powershell
python scripts/context/manage.py --config .ocr-context/config.json serve
python scripts/context/manage.py --config .ocr-context/config.json index --repository 1 --revision <完整SHA>
python scripts/context/manage.py --config .ocr-context/config.json wiki-path --repository 1 --branch master
```

部署、隔离边界及 Wiki 元数据见 [独立上下文服务](../integrations/context-service/README.md)。
