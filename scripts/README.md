# 项目脚本

脚本按用途归类，下面的命令从项目根目录执行。

```text
scripts/
├── context/
│   └── manage.py                 # 独立上下文服务、CodeGraph 索引、Wiki 目录
├── repositories/
│   ├── manage.py                 # 仓库列表、拉取更新、分支查看
│   └── requirements.txt          # 仓库维护的 Python 依赖
├── workbench/
│   └── start-web.ps1             # Windows 工作台启动与令牌配置
├── toolchain/
│   └── go.ps1                    # Windows 隔离 Go 工具链
├── checks/
│   └── verify-english-only.go    # 英文源码检查
├── licenses/
│   ├── add-license.sh           # 添加许可证头
│   └── verify-license.sh        # 检查许可证头
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
.\scripts\workbench\start-web.ps1
.\scripts\workbench\start-web.ps1 -Gitea -Restart
.\scripts\toolchain\go.ps1 version
.\scripts\toolchain\go.ps1 build -o .\dist\ocr-web.exe .\cmd\ocr-web
```

## 源码检查与许可证

```powershell
.\scripts\toolchain\go.ps1 run scripts/checks/verify-english-only.go
```

```bash
bash scripts/licenses/verify-license.sh
bash scripts/licenses/add-license.sh
```

新增脚本放入对应用途目录；依赖清单随所属工具存放。

## 可选上下文服务

```powershell
python scripts/context/manage.py --config .ocr-context/config.json serve
python scripts/context/manage.py --config .ocr-context/config.json index --repository 1 --revision <完整SHA>
python scripts/context/manage.py --config .ocr-context/config.json wiki-path --repository 1 --branch master
```

部署、隔离边界及 Wiki 元数据见 [独立上下文服务](../integrations/context-service/README.md)。
