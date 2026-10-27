# 构建与验证

应用操作见 [README](README.md)。四遥协议与工程兼容代码位于 `internal/core/`，桌面桥接位于根目录；`frontend/app.js` 管理界面，`frontend/forms.js` 保存表单字段与校验，样式统一放在 `frontend/app.css`。

## 环境

- Go 1.25+、Node.js 22+、Python 3.9+。
- macOS：Xcode 或 Command Line Tools，以及可登录的桌面会话。
- Windows：Windows 10/11 x64、WebView2、PowerShell，打包需 `python -m pip install pefile`。
- 重新生成图标时另需 `resvg-py` 和 `Pillow`；日常构建使用已有图标。

## 开发检查

```bash
npm ci
npm run check
npm run test:ui
python3 -m unittest discover -s tests -p 'test_*.py' -v
go test ./... -count=1
go vet ./...
```

`npm run format` 统一源码格式。CI 在 macOS、Windows 上执行上述检查，macOS 另做竞态检查。

## 生成交付包

macOS：

```bash
bash build-macos.sh
```

需要指定 Command Line Tools 时，使用 `DEVELOPER_DIR=/Library/Developer/CommandLineTools bash build-macos.sh`，无需修改全局工具设置。

Windows：

```powershell
npm ci
.\build.ps1
python scripts/package_protocol.py
```

构建脚本执行协议、界面、打包及原生窗口检查，记录源码、二进制和日志身份。macOS 还检查串口 PTY、竞态和 Windows 交叉构建；Windows 使用 `build/windows/resources.ps1` 写入资源。原生检查覆盖25个有效场景，合并窗口的历史入口不重复测试。

唯一当前交付为 `outputs/protocol-simulator/` 和 `outputs/protocol-simulator.zip`。`scripts/package_protocol.py` 校验当前构建后打包；完整记录保存在本地 `verification/`，不混入普通交付。应用包包含用户指南、小型示例、完整许可正文和构建摘要。

用户新增或修改的交付文件放入 `保留文件/`，以后更新继续保留。原版 GYTest、参考资料、用户工程和工作区外文件不参与清理。

## 规模与验证边界

按需生成大工程或执行额外压力检查：

```bash
GYLAB_SCALE_PROJECT=iec104 GYLAB_SCALE_OUTPUT=work/scale-projects go test ./internal/core -run '^TestScaleLargeProjectPersistence$' -count=1 -v
GYLAB_SCALE_PROJECT=modbus GYLAB_SCALE_OUTPUT=work/scale-projects go test ./internal/core -run '^TestScaleLargeProjectPersistence$' -count=1 -v
GYLAB_SCALE_NETWORK=1 go test ./internal/core -run '^(TestScaleConcurrentIEC104Channels|TestScaleModbusFullTablesMultiUnitAndBatchMappings)$' -count=1 -v -timeout=180s
GYLAB_SCALE_SOAK_DURATION=120s go test ./internal/core -run '^TestScaleAutomaticReportingRecovery$' -count=1 -v -timeout=180s
```

IEC104 工程档为64通道、640000点；Modbus 档为2通道、254装置、2540000点。生成与保存成功不代表全部点位都经过实时网络验收。

普通 `go test` 跳过需要桌面的原生检查；构建脚本通过 `GYLAB_NATIVE_SMOKE_BINARY` 启用。Windows 交叉编译不代表其原生界面通过，macOS Universal 包的运行结论以实际测试架构为准，串口 PTY 不代替物理 RS-485。

公司平台需另行核对可回连的 IP、已下发配置、IEC104 激活、命令确认、绑定反馈和属性回读。本地或 CI 通过不会改变历史实网测试中的未解决问题。
