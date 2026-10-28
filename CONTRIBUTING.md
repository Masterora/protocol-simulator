# 构建与验证

应用操作见 [README](README.md)。四遥协议与工程兼容代码位于 `internal/core/`，桌面桥接位于根目录；`frontend/app.js` 管理界面，`frontend/forms.js` 保存表单字段与校验，样式统一放在 `frontend/app.css`。

## 环境

- Go 1.25+、Node.js 22+、Python 3.9+。
- macOS：Xcode 或 Command Line Tools，以及可登录的桌面会话。
- Windows：Windows 10/11 x64、WebView2、PowerShell。
- Windows Setup 打包另需 NSIS 3.13 和 7-Zip。macOS 可用 `brew install makensis sevenzip`；Windows 安装 NSIS 和 7-Zip。工具未加入 PATH 时可设置 `MAKENSIS`、`SEVENZIP` 为可执行文件路径。WebView2 官方引导程序首次构建时下载并缓存到 `work/installer-runtime/`；Windows 构建还会验证其微软 Authenticode 签名。
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

`npm run format` 统一源码格式。CI 覆盖上述检查，macOS 另做竞态检查；Windows 另验证 Setup 的安装、启动、升级和卸载。

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

构建脚本执行协议、界面、打包及原生窗口检查，记录源码、二进制和日志身份。macOS 还检查串口 PTY、竞态和 Windows 交叉构建；Windows 使用 `build/windows/resources.ps1` 调用与 macOS 交叉构建共用的资源工具。版本号仅在 `wails.json` 的 `info.productVersion` 中维护，应用元数据、Windows 数字/字符串版本、清单和 Setup 均由此生成。原生检查覆盖25个有效场景，合并窗口的历史入口不重复测试。

`outputs/` 发布 macOS、Windows 安装包及 Linux 对应架构的服务器压缩包和桌面 DEB。macOS 包为 `protocol-simulator.dmg`，Windows 包为 `protocol-simulator-Setup.exe`。macOS 镜像包含 Universal `.app` 和 Applications 快捷入口；指南、示例、许可和构建摘要位于“资料”文件夹。暂存文件位于 `work/`，打包完成后清理。

`scripts/package_protocol.py` 校验当前构建后打包。NSIS 安装器按用户安装，在改动应用前检测 WebView2；已有应用运行或依赖安装失败时退出。升级保留已有示例，卸载只清理明确的程序文件，不递归删除安装目录，也不移除 WebView2 共享运行时。打包后用 7-Zip 解包核对程序和引导程序哈希，并验证安装器的图标、版本资源。安装器当前未做 Authenticode 签名。

macOS 使用 `hdiutil` 生成只读 DMG，验证校验和、挂载内容、快捷入口、可执行权限及复制安装后的签名。应用和挂载卷内嵌品牌图标；DMG 文件本身的自定义图标使用 macOS 扩展属性，经下载或不保留元数据的复制后可能恢复系统磁盘图标。Setup 和 DMG 都通过验证后才发布；详细记录位于 `verification/`。

在没有本软件安装记录的专用 Windows 测试账户中验证安装、启动、运行中拒绝升级/卸载、覆盖升级与工程保留：

```powershell
.\tests\windows_installer_smoke.ps1 -Setup outputs\protocol-simulator-Setup.exe -Application build\bin\protocol-simulator.exe
```

测试运行前会拒绝修改已有安装和快捷方式；成功后写入 `verification/windows-installer-runtime.json`。安装包交叉编译或解包成功不代表这项 Windows 实机验收已通过。

迁移旧交付目录时，只删除与原文件清单哈希一致的生成文件，用户新增或修改的文件保留原位。原版 GYTest、参考资料、用户工程和工作区外文件不参与清理。

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

实网验收需核对可回连的 IP、平台配置、IEC104 激活、命令确认、绑定反馈和属性回读。

## Linux 服务器构建

运行 `python3 scripts/build_linux_server.py`，先执行核心与 runner 的竞态测试及静态检查，再生成 amd64/arm64 的服务器压缩包。程序名为 `prs`，版本来自 `wails.json`，不链接 Wails/GTK/WebKit。运行默认热加载工程，校验失败保留旧配置；只重启变更通道并隔离其他工程。交叉构建与 Linux 实机、systemd 验收分别记录。

## Linux 桌面构建与验收

在 Ubuntu 24.04 目标架构环境安装 Go、Python、C 编译工具、pkg-config、GTK3/WebKitGTK 4.1 开发包和 dpkg-dev，然后执行：

```bash
python3 scripts/build_linux_desktop.py
go test -c -tags desktop,production,webkit2_41 -o /tmp/protocol-native.test .
sudo python3 scripts/verify_linux_desktop.py --package outputs/protocol-simulator-desktop-linux-amd64.deb --smoke-binary /tmp/protocol-native.test --output verification/linux-desktop-runtime --user "$USER"
```

验收另需 Xvfb、dbus-x11、xdotool、x11-utils 和中文字体。在没有既有 protocol-simulator 桌面安装的专用测试账户运行；测试会安装程序、以普通用户启动真实窗口并操作保存文件，结束后卸载且检查工程保留。不覆盖系统 `ps`。当前 Linux 自动验收使用 X11/Xvfb，Wayland 需另做实际验证。

服务器与桌面共享 `internal/core/`，服务器入口为 `cmd/prs/`，桌面入口继续使用根目录。Linux 专用窗口图标和分组配置通过平台文件隔离，其他平台返回原有默认值。Linux 交付版本也来自 `wails.json`；本地 DEB 在未配置作者邮箱时使用 `noreply@localhost`，正式分发前可配置作者联系信息。

## 服务器终端界面验收

`internal/observe/` 提供本机所属用户的观测与数据点操作接口，`internal/tui/` 只包含客户端界面；不持有引擎生命周期。手动操作只修改运行时数据点，不写工程文件、不暂停自动规则。通过 `python3 scripts/verify_tui_posix.py --binary PATH_TO_PRS --output verification/tui-runtime` 在 macOS/Linux 的真实 PTY 中验证双工程持续通信、搜索、点位分页、报文与日志、暂停、终端恢复、退出/Ctrl+C/崩溃和观测连接丢失隔离。验收使用 Python 标准库，无需图形桌面。
