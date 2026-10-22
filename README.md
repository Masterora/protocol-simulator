# protocol-simulator

Go + Wails 规约模拟工具，支持 IEC104、Modbus TCP / RTU、TCP / UDP / 串口。

源码位于当前目录；Windows 应用名为 `protocol-simulator.exe`，macOS 应用名为 `protocol-simulator.app`。两端共用同一份界面、协议引擎和多窗口服务，保留原有深色布局与交互；窗口标题栏使用各平台原生控件。

## 构建与验证

需要 Go、Node.js 和 Python；Windows 运行环境需要 WebView2。

```powershell
npm ci
.\build.ps1
python -m pip install pefile
python scripts/package_protocol.py
```

构建缓存位于 `work/golang`，验证日志位于 `verification`。
打包后仅保留 `outputs/protocol-simulator` 和 `outputs/protocol-simulator.zip` 作为当前发布。

`references` 保存原版逆向分析及工具参考资料，不参与应用构建。
项目迁移前的历史辅助脚本集中归档为 `references/legacy-work.zip`。

## macOS 构建与使用

需要 macOS 12.3 或更新版本、Go 1.25 或更新版本、Node.js、Python 3 和 Xcode Command Line Tools，在已登录桌面的 macOS 中执行：

```bash
bash build-macos.sh
```

脚本执行 Go 测试、`go vet`、并发竞态检查和 界面回归测试；通过两个隔离伪终端验证实际 macOS 串口驱动下的 RAW 双向通信、Modbus RTU 01/02/03/04 读、05/06 写、多装置隔离与停止取消。构建包含 arm64 / x86_64 的 Universal 应用并嵌入原应用图标，再启动 10 类真实原生弹窗检查前端桥接、可见控件和横向溢出。最终通过 `scripts/package_protocol.py` 核对烟雾测试的二进制哈希、全部前端嵌入、架构、签名，以及 ZIP 解压后的内容、执行权限与签名，更新当前交付：

- `outputs/protocol-simulator/protocol-simulator.app`
- `outputs/protocol-simulator.zip`

双击 `.app` 启动，也可拖入“应用程序”。该本地版本采用 ad-hoc 签名，未进行 Apple 公证；通过网络传输后如系统拦截，可在“系统设置 → 隐私与安全性”中允许打开。支持 Command+S 保存、右键点表菜单；串口设备使用 `/dev/cu.*` 路径。主窗口关闭即退出所有自有窗口与后台任务。

macOS 使用系统 WebKit，Windows 使用 WebView2；字体渲染和系统标题栏存在平台差异。业务界面的 HTML、CSS、控件位置和行为直接共用，不另做一套布局。交付目录只保留一份当前发布，包含 macOS 应用及已交叉构建验证的 Windows EXE；更新时按 manifest 保留用户新增或修改的工程及配置。

回归覆盖启动与默认停止状态、主/子站切换、通信管理直接编辑、参数变更后停止与手动连接、装置默认点数、五列表格编辑/取消/草稿保持、右键菜单、布局偏好、报文暂停、工程/CSV、IEC104 总召与遥信遥测、选择/执行/撤销、自动遥控遥调与自动数据，以及 Modbus TCP/RTU、TCP/UDP 本机回环。旧 WebKit 缺少 `requestSubmit`、`structuredClone`、`Array.at` 时的兼容路径另有针对性测试。最低系统版本 12.3 对应界面使用的 `:has()` 与原生 dialog 支持；未将当前机器实测等同于全部历史系统验证。

本机实际运行验证在 Apple 芯片 macOS 上完成；Intel 架构已构建并核对，未在 Intel 实机运行。伪终端验证不代表物理 USB 转串口、RS-485 时序、电气信号或现场设备验收。Windows 端源码共用及交叉编译检查不代表 Windows 实机逐项对照验收。

新建工程在独立主窗口进程中运行，每个工程拥有自己的协议引擎、连接、自动任务、配置弹窗和保存路径。关闭一个工程只退出其自有窗口和任务。右上角在尚无保存路径或存在未保存改动时显示“工程名（未保存）”，保存成功后去掉标记。

## 本轮功能对齐

已有装置配置现在可以修改：先校验，再停止活动通道、保存配置；不会自动重连。参数修改保留点值、描述、品质和手动增加/移动的点；改变点表范围时保留仍在范围内的已有数据。

104 主站支持周期总召、周期脉冲召唤、手动/周期校时、命令超时重发；子站支持累计量、双点遥信、步位置，以及 9/11/13/21/34/35/36 遥测格式和 SQ0/SQ1。链路可配置发送窗口、累计 I 帧确认数量、S 帧最长等待、U 测试周期和地址字段长度。手动遥控/遥调可附加 CP56 时标；子站可配置正常、否认、不应答及延迟策略，停止通道会丢弃待发送应答。

SOE/COS 事件窗口保留设备时间、时间无效标志、接收时间与品质，可按类别清除；缓存上限 5000 条，窗口显示最近 500 条。网络接收本身不标记工程为未保存。

Modbus 支持周期采集、多装置隔离、按配置地址读取事件寄存器，以及 16/32 位有符号/无符号数、32 位浮点和 AB/BA/ABCD/BADC/CDAB/DCBA 字节序。点地址以寄存器为单位，32 位点占用两个寄存器。RTU 校验码字节方向可选；位数据仍按标准 Modbus 位打包，AB 对每个16位块交换字节。

Modbus 校时使用显式配置的四寄存器映射：CP56Time2a 七字节加一个零填充字节；子站需启用“校时寄存器”，主站配置同一首地址。校时只改变模拟装置时间，绝不修改操作系统时间。保护装置事件读取保留原始寄存器记录，厂商私有事件编码不作猜测。

工程 JSON 保留新增配置和点元数据；CSV 兼容原五列及 feedback 列，按需追加 tab、wireType、timestamp、timeInvalid、dataType、byteOrder。

macOS 发布执行 `bash build-macos.sh`。Windows 原生发布继续通过 `build.ps1` 调用 `build/windows/resources.ps1`；macOS 可执行 `python3 scripts/build_windows_cross.py` 交叉构建 Windows x64，读取同一 PowerShell 资源定义并逐项核对图标、版本及 manifest。交叉构建不等于 Windows 实机验证。
