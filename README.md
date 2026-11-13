# protocol-simulator

IEC104、Modbus TCP/RTU 规约调试与模拟工具。桌面版提供点表、四遥操作、自动任务和报文界面；Linux 服务器版 `prs` 通过 JSON 工程运行，支持配置热加载、日志和 systemd。

- [安装](#安装)
- [桌面版](#桌面版)
- [服务器版 prs](#服务器版-prs)
- [工程 JSON 与点位](#工程-json-与点位)
- [自动数据与上报规则](#自动数据与上报规则)
- [完整示例](#完整示例)
- [热加载与多工程](#热加载与多工程)
- [Docker 部署](#docker-部署)
- [日志](#日志)
- [systemd 托管](#systemd-托管)
- [支持范围与开发](#支持范围与开发)

## 安装

| 系统                           | 直接下载                                                                                                                                                              | 运行要求          |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| macOS（Apple Silicon / Intel） | [protocol-simulator.dmg](https://github.com/Masterora/protocol-simulator/releases/latest/download/protocol-simulator.dmg)                                             | macOS 12.3+       |
| Windows x64                    | [protocol-simulator-Setup.exe](https://github.com/Masterora/protocol-simulator/releases/latest/download/protocol-simulator-Setup.exe)                                 | Windows 10/11     |
| Linux 桌面 amd64               | [protocol-simulator-desktop-linux-amd64.deb](https://github.com/Masterora/protocol-simulator/releases/latest/download/protocol-simulator-desktop-linux-amd64.deb)     | Ubuntu 24.04，X11 |
| Linux 桌面 arm64               | [protocol-simulator-desktop-linux-arm64.deb](https://github.com/Masterora/protocol-simulator/releases/latest/download/protocol-simulator-desktop-linux-arm64.deb)     | Ubuntu 24.04，X11 |
| Linux 服务器 amd64             | [protocol-simulator-server-linux-amd64.tar.gz](https://github.com/Masterora/protocol-simulator/releases/latest/download/protocol-simulator-server-linux-amd64.tar.gz) | 无需图形环境      |
| Linux 服务器 arm64             | [protocol-simulator-server-linux-arm64.tar.gz](https://github.com/Masterora/protocol-simulator/releases/latest/download/protocol-simulator-server-linux-arm64.tar.gz) | 无需图形环境      |

完整版本与更新说明：[GitHub Releases](https://github.com/Masterora/protocol-simulator/releases)。下载上表的安装包即可使用；GitHub 自动生成的 Source code 是源码。可用同一 Release 中的 `SHA256SUMS` 校验下载文件。

- **macOS**：打开 DMG，将应用拖到 Applications，推出镜像后从“应用程序”启动。指南和示例位于镜像的“资料”文件夹。当前使用本地签名；如系统阻止打开，可在“系统设置 → 隐私与安全性”中允许打开。
- **Windows**：运行 Setup，按当前用户安装，并创建快捷方式和卸载入口。缺少 WebView2 时会联网安装微软运行时。
- **Linux 桌面**：在下载目录运行 `sudo apt install ./protocol-simulator-desktop-linux-amd64.deb`，ARM64 改用 arm64 包，再从应用菜单启动。覆盖升级使用同一命令；卸载用 `sudo apt remove protocol-simulator`，用户自行保存的工程保留。
- **Linux 服务器**：可直接解压运行，或按 [当前用户安装](#当前用户安装与用户服务) / [系统级安装](#系统级安装与服务) 常驻运行；命令名为 `prs`，不影响系统 `ps`。

服务器包只含程序、说明、服务模板和示例，不包含其他服务器的工程。迁移时额外复制工程 JSON，例如现有部署的 `~/.config/protocol-simulator/gateway.json`；检查监听地址、端口、串口路径，并让网关连接新服务器 IP。`x86_64` 选择 amd64 包，`aarch64` / `arm64` 选择 arm64 包。

## 桌面版

### 快速开始：模拟 IEC104 子站

1. 启动后在顶部切换到“子站”。打开“通道设置”，选择 TCP Server，填写监听 IP 和端口。
2. 点击装置列表右侧的 **＋**，选择 IEC104，填写公共单元地址和四遥点位。遥测默认使用短浮点。
3. 在遥信或遥测页打开“自动上报配置”，点击 **＋** 添加规则，填写类型、点位、周期与方式，保存。新工程和新装置默认没有规则。
4. 点击“连接”启动子站监听，让主站按相同的 IP、端口、公共单元地址和点位类型接入，并激活 IEC104 链路。
5. 在主界面点击“开启自动上报”，在主站核对对应点位的数据，再保存工程以便下次使用。

配置窗口只保存参数，任务从主界面手动启动。IEC104 需要完成链路激活；只有 TCP 连接不代表可以上报数据。

无需外部主站时，可打开[本机四遥联调示例](examples/automatic-loopback.json)：先在子站连接示例服务端，再切换到主站连接示例客户端。载入工程不会自动连接或启动任务。

### 常用操作

- **装置与通道**：双击装置或从右键菜单进入配置；通道从右键菜单删除，并确认删除目标。
- **遥控、遥调**：IEC104 主站支持选择、执行、撤销、直控和自动任务；子站可将遥控绑定到遥信，将遥调绑定到遥测，执行后修改绑定值并反馈。选择、撤销、否认和不应答不会改变绑定值。

桌面端的遥控、遥调页均显示“绑定点位”列。子站可点击工具栏或表格中的“绑定点位”编辑绑定，主站的该列只读，未配置时显示 `—`。

- **Modbus 数据**：主站读取线圈、离散量和寄存器，写入线圈或保持寄存器；子站的“自动数据配置”修改点值，由主站轮询读取。
- **点表**：单击单元格编辑，Enter 提交，Esc 取消。搜索匹配点位或描述，每页最多 100 条；批量点位可写成 `1，3-5，8`。
- **通信**：有效的配置更改会断开对应通道，之后手动重连。在线修改点值、品质或批量设值保持连接；主站本地点值编辑不发送协议帧。
- **报文**：按连接地址、方向和通道筛选，可导出 CSV。暂停显示不会停止通信。

### 点表导入导出

选中装置，在点表工具栏打开“导入 / 导出”。支持 CSV、TSV（`.txt` 也按制表符读取）、JSON 和 Excel XLSX。CSV/TSV 使用 UTF-8，可带 BOM；XLSX 读取第一个工作表，只接受数值或文本单元格，公式请先粘贴为数值。不支持旧版 `.xls` 和缺少格式说明的原工具 C2 文件。

1. 选择文件和缺省类型；文件包含类型列时使用文件中的类型。
2. 选择“合并，同地址覆盖”“替换所选类型”或“替换装置全部点表”。预览显示新增、覆盖、删除数量及前 100 行；确认前校验全部点位、地址、数值和关联配置。
3. 确认导入后，仅停止当前通道，其他通道和工程继续运行。保存工程，再手动重连。预览之后工程配置发生变化，需要重新预览。
4. 导出可选装置全部点位或当前分类，再选 XLSX、CSV、TSV 或 JSON，指定保存位置。

同类型、同地址视为同一个点位。可用表头如下；`kind` 和 `address` 为核心列，缺少 `kind` 时使用界面选择的缺省类型，其余列可省略。

| 列                                                       | 含义 / 示例                                                                                    |
| -------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `kind` / 类型                                            | `yx` 遥信、`yc` 遥测、`yk` 遥控、`yt` 遥调；Modbus 使用 `coil`、`discrete`、`holding`、`input` |
| `address` / 点号                                         | 十进制、`0x4001` 或 `4001H`                                                                    |
| `name` / 描述                                            | 点位名称，可含中文                                                                             |
| `value` / 数值                                           | 有限数值，例如 `220.5`                                                                         |
| `quality` / 品质                                         | 品质字节，默认 0                                                                               |
| `wireType` / 104类型                                     | 例如短浮点遥测 `13`，必须符合装置协议                                                          |
| `dataType`、`byteOrder`                                  | Modbus 数据类型、字节序，按装置支持的格式填写                                                  |
| `tab`、`timestamp`、`timeInvalid`、`feedback`、`updated` | 分类、时标、时间无效标志、反馈、更新时间；可由本工具导出后保留                                 |

示例：[desktop-points.csv](examples/desktop-points.csv)。JSON 点表是点位对象数组，不是完整工程；完整工程仍通过“打开”载入。导出保留点位元数据。CSV/TSV 对可能被表格软件识别为公式的文本加单引号，本工具重新导入时还原。

### 工具箱

顶部“工具箱”的每个分类分别保留输入和选项。可用“填入示例”载入对应格式，也可按 Ctrl+Enter（macOS 上也可用 ⌘+Enter）运行。结果可完整复制。

| 分类               | 可用功能                                                                                                                                                                                                                                                                   |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 数值转换           | 整数进制、位掩码分析、2/4/8 字节寄存器的有符号和无符号解释、Float32/64 双向转换。4 字节支持 ABCD、DCBA、CDAB、BADC                                                                                                                                                         |
| 编码转换           | UTF-8、GBK、GB18030、UTF-16LE/BE 与 HEX 互转                                                                                                                                                                                                                               |
| 文本处理           | HEX 格式化、偏移转储、字节倒序、Base64 与文本或 HEX 互转、URL、JSON 格式化和压缩、字符串转义                                                                                                                                                                               |
| 时间戳             | 实时 Unix 时间戳，默认毫秒，可切换秒。与日期时间双向转换，默认 UTC+8，可切换 UTC                                                                                                                                                                                           |
| 报文解析 · IEC 104 | 解析一帧标准 HEX APDU，分组显示帧头、控制域、ASDU、点值、品质、时标和控制语义。也可单独解析 CP56Time2a                                                                                                                                                                     |
| 报文解析 · Modbus  | RTU / TCP / ASCII 单帧解析，分别选择请求或响应。常用功能码 01–06、0F、10、16、17 显示字段，其他功能保留原始数据区。可按 01–04 表单生成读取报文，也可输入站号、功能码和数据进行封装。“带入解析”会保留报文格式，自动解析生成的读取请求，或将通用封装结果填入解析区供选择方向 |
| IPv4 网段          | 输入 CIDR，例如 `192.168.1.25/24`，查看网络、掩码、广播地址和可用范围                                                                                                                                                                                                      |

例如生成 Modbus RTU 读保持寄存器请求，填站号 `1`、起始点位 `0`、数量 `1`，得到 `01 03 00 00 00 01 84 0A`。所有转换均在本地执行。

参考 [CyberChef](https://gchq.github.io/CyberChef/)、[Modbus 官方规范](https://www.modbus.org/modbus-specifications)、[Wireshark Modbus 字段参考](https://www.wireshark.org/docs/dfref/m/modbus.html)。

### 服务器工具

桌面顶部的「服务器」提供三个入口：

- **报文日志**打开 `prs run --frames` 生成的 JSONL 或轮转文件。可按时间、通道、方向或内容筛选，点击一条记录查看完整原文和解析结果。Modbus 解析需指定 TCP 或 RTU，以及请求或响应。
- **部署检查**校验当前工程，检查工程内监听冲突、监听地址和目标服务器 IP 是否一致，列出需要现场核对的串口及端口，并提示已启用规则的启动选项。
- **SSH 查看**使用系统 OpenSSH 和现有密钥读取运行工程、通道、装置及近期日志。可从下拉列表选择 OpenSSH 别名，也可在「管理主机」保存常用目标和显示名称。主机必须已记录在 `known_hosts`，远端账号须能访问运行工程的本机观测套接字，且安装支持 `prs inspect` 的版本。

### 保存与更新

- “保存”将通道、装置、点表和规则写入 JSON。工程文件请放在文稿等可写目录，不要保存在只读 DMG 中。
- 新建工程打开独立窗口，各工程的连接与任务互不影响。打开已有工程后，连接和任务均需手动启动。
- “更新”检查 GitHub 正式版本并打开下载页，不自动下载安装。更新前保存工程并关闭旧版窗口；macOS 拖入新版并替换，Windows 运行新版 Setup，再启动并打开工程。安装器不会强制关闭运行中的工程。
- Windows 安装版可从系统的应用列表卸载。卸载保留工程文件、示例工程和 WebView 缓存；已有同名示例在升级时不会覆盖。

### 联调检查

- **已连接却没有数据**：检查主站配置、链路激活、公共单元地址、点位类型及上报开关。使用 VPN 时，监听地址应选主站能回连的网卡 IP。
- **遥控、遥调没有反馈**：检查绑定点位，并查看“运行详情”中的执行确认、执行结束和反馈报文。
- **归一化数值越界**：归一化遥测要求 `-1 ≤ 值 < 1`；100、220.5 等数值应选择短浮点或适用的标度化格式。

## 服务器版 prs

### 解压与快速运行

以下在解压得到的 `protocol-simulator-server/` 目录执行。ARM64 将包名中的 `amd64` 换成 `arm64`。

```bash
tar -xzf protocol-simulator-server-linux-amd64.tar.gz
cd protocol-simulator-server
./bin/prs version
./bin/prs validate examples/server-iec104.json --json
./bin/prs run examples/server-iec104.json --start-data-rules --log-format json
```

示例默认监听本机；远程主站接入时将工程的监听地址改为可回连的明确网卡地址，例如 `192.168.1.10:24040`。主站连接同一个服务器 IP。

`run` 默认启动全部通道并开启热加载，自动数据规则默认不启动，需显式加上 `--start-data-rules`。工程文件只读，运行点值不会写回 JSON。前台按 Ctrl+C 或发送 SIGTERM 正常退出并释放连接、端口和串口。TCP Client在未连接时持续等待和重试，直到停止。

### 命令参考

以下示例假定已按 [systemd 托管](#systemd-托管) 安装命令链接；未安装时，将 `prs` 换成解压目录中的 `./bin/prs`。

| 命令                                                          | 用途                                       |
| ------------------------------------------------------------- | ------------------------------------------ |
| `prs` / `prs help` / `prs -h` / `prs --help`                  | 完整帮助                                   |
| `prs version` / `prs --version`                               | 版本                                       |
| `prs ports`                                                   | 列出串口名称                               |
| `prs ports --json`                                            | 以 JSON 数组列出串口                       |
| `prs validate project.json`                                   | 校验工程，不打开网络端口或串口             |
| `prs validate project.json --json`                            | 以 JSON 输出校验结果                       |
| `prs tui`                                                     | 终端界面，查看工程、修改运行点值及立即上报 |
| `prs tui --runtime-dir DIR`                                   | 指定工程 socket 目录                       |
| `prs point set`                                               | 修改选定工程、装置、点位的运行值或品质     |
| `prs point report`                                            | IEC104 子站立即上报所选点的最新值          |
| `prs tui --help`                                              | TUI 参数帮助                               |
| `prs run project.json`                                        | 运行一个工程                               |
| `prs ports --help` / `prs validate --help` / `prs run --help` | 子命令参数帮助                             |

直接运行 `prs` 或 `prs --help` 查看中文指令集。只输入命令组（例如 `prs channel`）会列出该组操作。`prs help run` 等同于 `prs run --help`，`prs help point report` 查看单个操作的参数。

安装后运行 `man prs` 查看命令手册。在解压目录也可以运行 `man -l share/man/man1/prs.1`。

`validate` 和 `run` 也可写成 `prs validate --project project.json`、`prs run --project project.json`。一个命令只提供一个工程路径，不要同时使用位置参数和 `--project`。推荐将文件路径放在子命令后、其他参数前。

### 主站联调

先启动主站通道，使用 `prs inspect` 查看工程 PID、通道标识和装置标识，再按需要执行下列命令。

```bash
prs master interrogate --pid 1234 --channel iec-client --device device-1
prs master read --pid 1234 --channel modbus-client --device device-1 --function 3 --points 0,2-5
prs master write --pid 1234 --channel modbus-client --device device-1 --function 6 --points 0 --value 42
```

读取支持功能码 1–4，写入支持 5、6、15、16。线圈值填 0 或 1，寄存器值填 0–65535。结果可在 TUI 点位和报文页核对。主站操作通过工程的本地观测套接字执行，运行中的工程进程须升级到支持该接口的版本。

### run 的全部参数

| 参数                             | 默认值   | 说明                                                               |
| -------------------------------- | -------- | ------------------------------------------------------------------ |
| `--runtime-dir DIR`              | 自动选择 | 本机所属用户的工程 socket 目录，供 `prs tui` 连接                  |
| `--project FILE`                 | 无       | 工程 JSON，替代位置参数                                            |
| `--channels all`                 | `all`    | 全部通道；也可用 `ID1,ID2` 选择，使用 JSON 中的 `id`，不是显示名称 |
| `--start-data-rules`             | `false`  | 启动已保存且 `enabled: true` 的自动数据/上报规则                   |
| `--watch=true` / `--watch=false` | `true`   | 开关工程文件热加载                                                 |
| `--log-format text`              | `text`   | `text` 或 `json` 运行日志                                          |
| `--frames FILE`                  | 不保存   | 原始收发报文 JSONL 文件                                            |
| `--frames-max-mb 10`             | `10`     | 每个报文日志上限，单位 MiB，范围 1–1024                            |
| `--frames-files 3`               | `3`      | 包含当前文件在内的保留文件总数，范围 1–10                          |

布尔参数关闭时使用 `--watch=false`、`--start-data-rules=false`，不要写成 `--watch false`。`ports` 和 `validate` 的 `--json` 默认为 false；`run` 使用 `--log-format json`。

```bash
prs run project.json \
  --channels channel-1,channel-2 \
  --start-data-rules \
  --log-format json \
  --frames /var/tmp/protocol-frames.jsonl \
  --frames-max-mb 10 \
  --frames-files 3
```

退出码：`0` 正常退出，`2` 参数或工程预检失败，`1` 启动、运行或日志写入失败。热加载被拒绝时通常继续使用旧配置，不会退出。

当前没有 `prs stop`、`prs status`。手动点值和上报使用 `prs point set`、`prs point report`。运行状态见日志；后台启停由 systemd 管理。服务器入口不会启动自动遥控、自动遥调任务。

### 终端界面：prs tui

先启动工程，再在另一终端进入界面：

```bash
prs run project.json --start-data-rules --log-format json
prs tui
```

界面只连接已有进程，不会重复启动工程。启动后自动选择有效工程、通道和装置；按数字键或 Tab 查看各页，切换工程时使用工程列表中的高亮行。每个工程使用独立 Unix socket；退出、Ctrl+C、界面崩溃或观测连接中断都不会停止工程通信。支持通过 SSH 使用，终端至少 60 列、12 行。

| 按键                     | 操作                                              |
| ------------------------ | ------------------------------------------------- |
| `1`–`7`、Tab / Shift+Tab | 切换工程、通道、装置、点位、规则、报文和日志页    |
| Enter / `i`              | 进入下一级或打开详情；`i` 查看当前行详情          |
| `n` / `e` / `x`          | 新增 / 编辑 / 删除配置                            |
| `c` / `r`                | 连接或停止通道 / 切换选中规则的运行状态           |
| `v` / `s`                | 修改子站数据点的值和品质 / 立即上报 IEC104 数据点 |
| `m` / `o`                | 点位页导入 / 导出点表                             |
| ↑↓、Home/End、PgUp/PgDn  | 逐行选择、跳到列表首尾、按屏幕可见行数翻页        |
| ←→                       | 切换点位分类、切换表单选项或选择删除确认项        |
| `/` / Esc                | 筛选；Esc 清除筛选、取消弹窗或返回上层            |
| `g`                      | 在报文和日志页恢复跟随                            |
| 空格 / Ctrl+U            | 打开当前字段的选项 / 清空可输入的字段或筛选内容   |
| `?`                      | 打开帮助                                          |
| `q` / Ctrl+C             | 退出界面，工程继续运行；输入和确认时用 Ctrl+C     |

点位页按 `m` 导入。填写服务器上的 CSV、TSV、JSON 或 XLSX 文件路径，选择合并、替换类型或替换全部，按 Enter 预览新增、覆盖和删除，再按 Enter 保存。自动规则或绑定仍引用的点位不能删除。按 `o` 导出当前分类或装置完整运行点表，目标文件必须是新文件。相对路径以启动 `prs tui` 的目录为准。升级 TUI 后，已运行的旧工程可导出点表，导入在新版工程进程上开放。

桌面自动规则编辑器的“下限 / 固定值”列：固定值模式使用该列作为固定输出值，区间模式使用该列作为下限。主窗口“开启/关闭自动上报（自动数据）”控制任务启动和停止，运行后可用“暂停任务/恢复任务”；报文区域的暂停只影响报文显示。

终端绑定操作：进入子站装置的点位页，选中遥控／遥调点位后按 `e`。在“编辑点位”中直接输入“绑定遥信点位”或“绑定遥测点位”，例如 `301-302`；不连续的点位写作 `301,311`。遥控的反向目标单独填入“反向遥信点位”，例如充电桩 `25001` 的反向目标为 `201`。留空取消该输入框对应的绑定；同一目标不能同时正向和反向绑定，所有目标必须已存在于同一装置。Modbus 不同寄存器类型分开输入，避免点位编号相同时混淆。`Enter` 将点位字段与绑定一起原子保存到工程 JSON 并生效，输入错误时保留草稿；`Esc` 取消。不新增绑定快捷键。绑定保存不会发送遥控、遥调命令，也不会重启通道或重置自动规则。点位表“绑定点位”列实时显示结果。IEC104 步调节归入遥调，配置保存在 `devices[].configuration.controls.yk/yt.mappings`，与桌面共用。

规则页用 `r` 切换当前选中的一条规则：运行中停止，已暂停时恢复，其他状态启动。筛选后仍只操作选中行；同装置其他规则的状态、执行次数和定时周期保持不变。状态列显示各条规则的实时状态，顶部提示随选中规则显示“启动规则”“停止规则”或“恢复规则”，无需菜单。快捷键只控制本次运行，不修改配置中的“启用”开关；即使配置未启用，也可手动启动所选规则，重启工程后仍按原配置。`s` 仅在点位页立即上报所选数据点。界面始终实时刷新。旧工程服务需升级并重新启动后才能单独控制规则。

字母快捷键不区分大小写，例如 `e` 与 `E` 均为编辑配置，`v` 与 `V` 均为改值；不再用大小写区分功能。名称、路径、数值和搜索文本保留输入原样。页面标签下方的一排快捷键同时显示当前页面操作和可用的通用操作，弹窗打开时切换为弹窗操作，按键用亮色区分。连接中的通道提示“停止”，停止的通道提示“连接”；窄终端优先保留详情、编辑、删除、改值、上报和分类，次要操作可在 `?` 帮助中查看。表单输入时提示字段编辑、保存和取消，避免把输入字母误认为全局命令。

通道列表的“角色”列显示主站或子站，“连接方式”列统一显示 TCP Client、TCP Server 或串口，支持按角色与连接方式搜索；选中通道后，顶部显示“通道名 · 主站/子站”，进入点位、报文等页面后仍可看到角色。角色取自工程的 `role` 字段，与 TCP 连接方向分别设置。报文与日志默认跟随最新记录，按上下方向键停止跟随，按 `g` 恢复。详情窗按所选记录的标识持续刷新并保持滚动位置，Esc 返回列表。

界面每 250 毫秒尝试从运行进程读取一次最新状态；请求延迟可能延长实际刷新间隔。底部显示所选工程的累计运行时长。自动生成、手动修改和协议接收产生的新值都随下一次刷新显示；界面不等待自动规则周期。自动规则周期仅决定何时产生下一次自动变化，手动操作不会暂停或重置该规则。列表与详情窗始终读取最新状态。点位、品质、通道状态和规则执行次数在详情中实时变化；报文、日志按记录编号保持选中，历史记录被淘汰时明确提示，不自动切换到另一条记录。

点位视图中，遥信、遥测、遥控、遥调独立显示，按左右方向键切换；Modbus 的保持寄存器、输入寄存器、线圈、离散输入也分别显示。各分类按点位编号升序排列，搜索仅作用于当前分类。PgUp/PgDn 每次翻一屏，30 行高的终端显示 21 条。后台按 100 条一批读取，跨批次仍可逐行选择或翻页。分类在点位表格标题栏显示，当前分类用颜色强调；各列表以无表头序号列编号，点位跨页连续编号，不显示数量和分页统计。当前页面与分类用颜色强调；顶部显示应用名称和快捷键，底部显示工程运行时长、操作或连接异常提示，右下角保留署名。运行时长显示为 `Nd HH:MM:SS`，例如 `0d 23:59:59` 后为 `1d 00:00:00`；从所选工程进程启动开始计算，切换工程时随之变化，关闭、重开查看器或跨午夜不会归零。点位列表显示实时值和品质，规则视图显示配置和各条规则的实时状态；报文视图显示 RX/TX 十六进制数据，长报文自动换行，Enter 查看详情，c 复制完整十六进制原文；日志视图显示运行事件、协议提示及热加载结果。历史有界，界面保留最近 1000 条记录；断开过久时可能跳过旧记录。

桌面端和 `prs tui` 点位表在“描述”前显示只读“上报时间”。它记录本次工程进程内最近一次该点位的实际通信：遥信、遥测的上报收发，遥控、遥调的命令或响应；Modbus 则记录对应点位的成功读写通信。相同数值再次通信也会更新时间，界面刷新、离线改值、未发出的排队报文不会更新时间；尚未通信显示 `—`。macOS、Windows、Linux 桌面端和服务器终端的所有装置共用此列，IEC104 四遥及 Modbus 各类点位统一显示 `HH:mm:ss`，不显示毫秒。终端默认使用系统时区，也可用 `TZ=Asia/Shanghai prs tui` 指定显示时区，按 Enter 查看完整日期和时间（`YYYY-MM-DD HH:mm:ss`）。桌面端沿用通信端的时区，悬停可查看完整日期和时区。此时间与报文内的事件时间分开，仅用于运行显示，不写入配置；重启工程后重新记录。

工程默认在 `PRS_RUNTIME_DIR` 指定目录创建 socket；未设置时使用 `$XDG_RUNTIME_DIR/protocol-simulator`，再回退到系统临时目录下的 `protocol-simulator-UID`。默认目录及 socket 仅所属用户可访问；自定义目录不得为符号链接，也不得允许组或其他用户写入。两端可以同时用 `--runtime-dir DIR` 指向同一目录。界面支持显式新增、编辑和保存工程配置，以及数据点修改和立即上报。配置编辑需要同一用户对工程文件及其目录有写权限；只读挂载仍可观测和立即上报，保存会明确报错。更新正在运行的服务进程后才能使用新增配置功能。

systemd 模板使用 `/run/protocol-simulator`，与 PrivateTmp 隔离无关。查看其工程时使用有权限的用户，例如：

```bash
sudo /opt/protocol-simulator/bin/prs tui --runtime-dir /run/protocol-simulator
```

模板会创建并保留共享的专用运行目录；停止某个实例只清理自己的 socket，其他实例继续运行。若界面没有列出工程，检查使用的用户、目录和工程日志中的 `observer_ready` / `observer_unavailable`。旧版本运行中的进程需要升级并重启后才会提供观测接口。

### 手动修改与立即上报

在点位页选中遥信、遥测或 Modbus 数据点，按 `v` 编辑数值和品质；数值可直接输入，品质与开关量可按空格打开选择列表；Tab / Shift+Tab / ↑↓ 切换字段，Ctrl+U 清空输入，Enter 保存，Esc 取消。仅提交实际编辑的字段，例如只改品质时保留后台最新数值。IEC104 子站在连接激活后立即上报；未连接时明确显示“已修改，未上报”。Modbus 不主动发报，主站下次读取时得到新值。

在 IEC104 子站的数据点上按 `s`，立即上报该点此刻的最新数值和品质，不改变点值，也不广播整个装置。服务器入口不下发遥控、遥调；对应点位不能通过此入口修改或发送。

自动规则保持运行。自动、手动和协议更新按实际发生顺序应用，列表和详情持续显示最新值；手动值随后可能被自动规则更新。显式点值和品质修改默认将所填字段原子保存到工程 JSON；只改品质不会把后台自动变化的数值一起保存。自动生成和协议接收仍只改变运行状态，不影响其他工程。CLI 可用 `--runtime-only` 明确选择不保存的点值修改。

脚本调用须明确指定运行工程 PID（见 TUI 工程页）、通道 ID、装置 ID、类型和点地址。下例使用 `examples/server-iec104.json` 的标识；将 PID 替换为实际值：

```bash
PROJECT_PID=12345
prs point set --pid "$PROJECT_PID" --channel iec104-server --device device-1 \
  --kind yc --address 16385 --value 221.5 --quality 0
prs point set --pid "$PROJECT_PID" --channel iec104-server --device device-1 \
  --kind yc --address 16385 --quality 0x80 --json
prs point report --pid "$PROJECT_PID" --channel iec104-server --device device-1 \
  --kind yc --address 16385 --json
```

`--value` 和 `--quality` 至少提供一项；品质为 0–255，地址支持十进制和 `0x` 十六进制。可用 `--runtime-dir DIR` 指定观测目录。命令自动读取当前配置版本；配置变更、目标消失或不合法数值会被拒绝。退出码 `2` 表示参数错误，`1` 表示操作失败；断网或上报部分失败时不会自动重试，请先核对最新状态。JSON 结果中的 `saved` 表示配置是否保存，`path` / `revision` 是文件路径和修订号；`applied` 表示运行值是否已修改，`receivers` 是实际上报成功的连接数，`mode` 区分 `reported`、`offline` 和 `polled`。

### 新增工程、通道、装置、点位与自动上报

在工程、通道、装置、点位、规则页按 `n` 新增，按 `e` 编辑选中项；点位页同样用 `e` 编辑点位、类型、描述和协议元数据，用 `v` 修改数值和品质。表单内 Tab / Shift+Tab / ↑↓ 切换字段，←/→ 快速切换选项，空格打开选择列表，列表内 ↑↓ / PgUp/PgDn 选择、Enter 确认、Esc 返回表单；表单内 Enter 保存、Esc 放弃；轮询不会覆盖输入。新装置每类默认 50 点，新上报规则默认禁用。规则周期范围为 200–3600000ms，点位范围可填 `1,3-8`；明确启用后才运行，Modbus 数值变化由主站读取。

新增通道默认使用当前主机活动网卡上的明确 IPv4 地址；未发现可用网卡时使用 `127.0.0.1`。通道监听地址可按空格选择目标服务器的网卡地址，保留原端口，也可直接编辑。迁移到其他主机后应选择新主机的地址。小终端中的弹窗会为当前字段保留空间，长输入显示正在编辑的末尾；省略号表示内容被截断。操作结果短暂优先显示在状态栏，随后恢复刷新时间。

协议、主站/子站、连接方式、串口参数、点位类型、104 报文类型、品质、寄存器格式、字节序和自动上报选项显示中文标签，通过选择填写。104 类型随点位类型联动，连接方式随协议联动；名称、IP/端口、点位、范围和数值仍可输入。波特率可选常用值，也保留自定义值；串口可选择本机已发现的端口。选择列表确认只修改表单草稿，按 Enter 保存表单后才生效；Esc 逐层取消，不保存。

在工程、通道、装置、点位和规则页按 `x` 删除选中项。确认窗口显示目标及影响范围，默认取消；切换到“永久删除”并按 Enter 才提交。删除不可恢复，不进入回收站。工程删除停止对应进程并删除工程文件；通道删除停止该通道，并删除其装置、点位及规则；装置删除包含其点位和规则。点位若仍被自动上报或关联引用，需先解除引用。其他工程和无关通道继续运行。确认后若配置已被其他操作修改，删除会被拒绝并要求刷新。旧服务需要升级运行进程后才能删除。

CLI 显式删除命令示例（工程删除必须使用 `--pid`；其他配置也可用 `--file` 离线保存）：

```bash
prs rule delete --pid "$PROJECT_PID" --channel CHANNEL_ID --device DEVICE_ID --index 1
prs point delete --pid "$PROJECT_PID" --channel CHANNEL_ID --device DEVICE_ID --kind yc --address 16385
prs device delete --pid "$PROJECT_PID" --channel CHANNEL_ID --device DEVICE_ID
prs channel delete --pid "$PROJECT_PID" --channel CHANNEL_ID
prs project delete --pid "$PROJECT_PID"
```

新工程保存在表单指定的 JSON 路径，启动独立后台进程，初始没有通道。在通道页按 `n` 新增，按 `c` 手动连接。退出 TUI 不会关闭工程。新增通道和修改通信参数的通道保持停止，点表、描述、点值和规则编辑保留现有传输连接；其他工程继续运行。

CLI 同样支持配置管理（将 PID、通道和装置标识替换为实际值）：

```bash
# 新建文件并启动独立工程，返回 PID；文件已存在时拒绝覆盖
prs project new ./工程2.json --name 工程2 --start --json
# 运行中的工程优先按 PID 操作，确认文件和运行状态同步
prs channel add --pid "$PROJECT_PID" --set name=通道2 --set address=127.0.0.1:2405 --json
prs device add --pid "$PROJECT_PID" --channel CHANNEL_ID --set name=装置1 \
  --set 'initialPoints={"yx":"1-10","yc":"111-150","yk":"201","yt":"301"}' --json
prs point add --pid "$PROJECT_PID" --channel CHANNEL_ID --device DEVICE_ID \
  --kind yc --address 101 --set count=5 --set name=温度 --json
prs point edit --pid "$PROJECT_PID" --channel CHANNEL_ID --device DEVICE_ID \
  --kind yc --address 101 --set name=出口温度 --set wireType=13 --json
prs rule add --pid "$PROJECT_PID" --channel CHANNEL_ID --device DEVICE_ID \
  --set kind=yc --set points=101-105 --set mode=report --set intervalMs=1000 --set enabled=true --json
prs rule edit --pid "$PROJECT_PID" --channel CHANNEL_ID --device DEVICE_ID \
  --index 1 --set intervalMs=2000 --json
prs channel start --pid "$PROJECT_PID" --channel CHANNEL_ID
prs channel stop --pid "$PROJECT_PID" --channel CHANNEL_ID
prs project edit --pid "$PROJECT_PID" --set name=现场工程
```

配置使用修订号检查，拒绝覆盖其他终端刚保存的修改；CLI 可传 `--revision SHA256` 固定预期版本。保存失败不修改运行状态；保存后上报失败会分别报告保存结果和通信结果。只保存显式字段及必要的格式规范化，不把自动变化的实时快照写入文件。

TUI 新增装置时每种类型占一行，并排填写起始点位和点数。Tab 切换字段，↑↓ 切换行。各类型点数默认 50，填 0 可跳过。类型由通道协议决定，IEC104 每通道一个装置。CLI 用 `initialPoints` 指定各类型的点位范围，原有 `pointKind`、`pointStart` 和 `pointCount` 仍支持单类型初始化。

通道表单的 IPv4 地址固定显示分隔符。可直接输入 `1921681001001000`，也可输入 `192.168.100.100:1000`。短段输入 `.` 跳到下一段，`:` 跳到端口。退格逐位删除，Ctrl+U 清空数字。表单中 Tab 到下一项，Shift+Tab 回上一项。

IEC104 子站在主站连接并完成 STARTDT 激活后可主动上报。点位页按 `s` 上报，连接未就绪时同一快捷键显示等待原因。

IEC104 表单默认起点为遥信 1、遥测 16385、遥控 24577、遥调 25089、累计量 25601、步位置 4097、步调节 24833。Modbus TCP 和 RTU 表单默认起点为离散输入 1、保持寄存器 1024、线圈 2560、输入寄存器 0。起点可按实际点表修改。

未运行的工程可以将 `--pid PID` 换为 `--file PATH` 离线编辑；该模式只确认文件保存，不确认运行状态。`prs run FILE --channels none` 可加载工程并保持所有通道停止。长期服务可按下文配置 systemd。Docker 内使用配置编辑时，应将配置**目录**以可写方式挂载，并给运行 UID 65532 文件及目录写权限；自动热加载和只读观测场景可继续使用只读挂载。

## 工程 JSON 与点位

可以在桌面版完成配置后保存为 JSON，也可以按下面格式编辑。新配置使用 `version: 2` 和 `devices` 列表；兼容读取旧版 `version: 1`。JSON 不支持注释或尾逗号，未知字段会被拒绝。完整例子见 [完整示例](#完整示例)。

### 工程、通道与装置

| 层级 | 字段                                     | 说明                                                                                          |
| ---- | ---------------------------------------- | --------------------------------------------------------------------------------------------- |
| 工程 | `version`                                | 格式版本，新配置填 `2`                                                                        |
| 工程 | `name`                                   | 非空工程名称                                                                                  |
| 工程 | `channels`                               | 通道数组，最多 64 个；默认选择全部通道时，空工程保持等待，之后可通过热加载添加通道            |
| 通道 | `id`、`name`                             | 稳定且在工程内唯一的 ID、非空显示名称；修改时保留 ID                                          |
| 通道 | `protocol`                               | `iec104`、`modbus-tcp`、`modbus-rtu`                                                          |
| 通道 | `role`                                   | `server` 子站或 `client` 主站；与传输连接方向分别设置                                         |
| 通道 | `transport`                              | `tcp-server`、`tcp-client`、`serial`，组合限制见下表                                          |
| 通道 | `address`                                | 网络端点，如 `127.0.0.1:24040`、`[::1]:24040`；串口则填 `/dev/ttyUSB0` 等设备路径             |
| 通道 | `localAddress`                           | TCP Client 的可选本地绑定地址，含端口；留空由系统选择                                         |
| 通道 | `timeoutMs`                              | 超时毫秒数，默认 `3000`，范围 100–60000                                                       |
| 通道 | `baud`、`parity`、`dataBits`、`stopBits` | 串口参数，默认 `9600`、`N`、`8`、`1`；波特率 50–4000000，校验 `N/E/O`，数据位 5–8，停止位 1/2 |
| 通道 | `devices`                                | 装置列表，最多 256 个；IEC104 每通道一个装置                                                  |
| 装置 | `id`、`name`                             | 装置 ID 在所属通道内唯一，名称非空                                                            |
| 装置 | `unit`                                   | Modbus 单元地址；RTU 子站为 1–247，主站 0 可用于广播写入                                      |
| 装置 | `commonAddress`                          | IEC104 公共单元地址，标准配置为 1–65534，需与主站一致                                         |
| 装置 | `points`                                 | 点表，最多 10000 点；同一装置的同种 `kind` 不能有重复或重叠地址                               |
| 装置 | `dataTests`                              | 自动数据与上报规则，推荐使用 `telemetry`，见下一节                                            |
| 装置 | `configuration`                          | 可选规约高级参数、点表范围和四遥绑定，建议由桌面版生成并保留                                  |

| protocol               | 可用 transport                                          |
| ---------------------- | ------------------------------------------------------- |
| `iec104`、`modbus-tcp` | `tcp-server` / `tcp-client`                             |
| `modbus-rtu`           | `serial`，也支持 TCP 字节流 `tcp-server` / `tcp-client` |

桌面端与服务器终端使用相同的连接方式文案：TCP Client、TCP Server、串口。TCP Server的 `address` 显示为“监听地址”；TCP Client 的 `address` 显示为“目标地址”；串口显示为“串口路径”。TCP Client的可选 `localAddress` 显示为“本地绑定”。主站/子站是协议角色，与客户端/服务端的 TCP 连接方向独立设置。网络地址必须包含主机和端口，串口运行前用 `prs ports` 确认设备及权限。

旧工程中用 RAW 表示的空 TCP 或串口通道，打开后分别按空 IEC104 或空 Modbus RTU 通道处理，保留原地址和角色。含 UDP 或 RAW 自动化配置的旧文件需先转换成支持的规约。

兼容字段：通道级 `points`、`unit`、`commonAddress` 用于旧工程，新配置优先放在 `devices` 中；`transportDraft` 是桌面传输表单草稿。装置级 `autoData` 是旧版统一自动数据格式；没有 `dataTests` 时仍会读取。不要将旧结构与新结构混写来覆盖同一份配置。

`configuration` 对象包含 `version`、`values`、`ranges`，以及可选的 `controls`、`relations`、`autoTests`。规约版本名称分别为 IEC104 的 `标准版`、Modbus TCP 的 `TCP版`、RTU 的 `标准版(RTU)`。四遥映射与高级字段的完整保存示例见 [automatic-loopback.json](examples/automatic-loopback.json)；服务器可加载配置，但不会自动执行其中的遥控、遥调任务。

### 点位字段

| 字段                       | 说明                                                                                 |
| -------------------------- | ------------------------------------------------------------------------------------ |
| `address`                  | 十进制点地址；IEC104 为 0–16777215，Modbus 为 0–65535，且受配置的地址字节数约束      |
| `kind`                     | 点类型，见下表                                                                       |
| `name`                     | 点描述，建议填写                                                                     |
| `value`                    | 初始数值，必须是有限数值；位点通常是 0/1，其他类型受编码范围约束                     |
| `quality`                  | 品质位掩码；`0` 表示正常，IEC104 按对应 ASDU 类型编码                                |
| `wireType`                 | IEC104 ASDU 类型，如遥信单点 `1`、双点 `3`，归一化遥测 `9`、标度化 `11`、短浮点 `13` |
| `dataType`                 | Modbus 寄存器类型：`uint16`（默认）、`int16`、`uint32`、`int32`、`float32`           |
| `byteOrder`                | 16 位用 `AB`（默认）/`BA`；32 位用 `ABCD`（默认）/`BADC`/`CDAB`/`DCBA`               |
| `tab`                      | 可选的桌面四遥分类；简单工程可省略，服务器按点类型推断                               |
| `timestamp`、`timeInvalid` | 带时标点的时间与时间无效标记；时间使用 RFC3339，如 `2026-10-28T10:00:00+08:00`       |
| `updated`、`feedback`      | 保存或运行时的更新时间、反馈元数据；手写基础配置可省略，已有工程保留原值             |

| 协议   | kind                 | 用途                            |
| ------ | -------------------- | ------------------------------- |
| IEC104 | `yx` / `yc`          | 遥信 / 遥测，支持自动数据与上报 |
| IEC104 | `yk` / `yt` / `step` | 遥控 / 遥调 / 步调节            |
| IEC104 | `mc` / `position`    | 累计量 / 步位置信息             |
| Modbus | `coil` / `discrete`  | 线圈 / 离散输入                 |
| Modbus | `holding` / `input`  | 保持寄存器 / 输入寄存器         |

Modbus 使用 PDU 的零基地址：保持寄存器地址 `0` 通常对应外部工具显示的 `40001`。32 位数占两个连续寄存器，避免重叠。`dataTests.telemetry` 面向遥信/遥测，Modbus 示例用 `holding` 自动变化；线圈点用于协议读写。

例如短浮点电压点：

```json
{ "address": 16385, "kind": "yc", "wireType": 13, "name": "电压", "value": 220.5, "quality": 0 }
```

归一化遥测要求 `-1 ≤ value < 1`。100、220.5 等工程量使用短浮点或适用的标度化类型；单点遥信用 0/1，双点遥信按对应状态编码。

## 自动数据与上报规则

规则位于 `channels[].devices[].dataTests.telemetry`。新工程默认没有规则；添加规则、设置 `enabled: true` 后，还必须以 `--start-data-rules` 启动。规则只作用于服务器子站支持的数据点，CLI 不会自动发送主站遥控或遥调。

```json
{
  "telemetry": {
    "stopOnDisconnect": false,
    "rules": [
      {
        "enabled": true,
        "kind": "yc",
        "points": "16385",
        "intervalMs": 1000,
        "mode": "report",
        "cause": 1
      }
    ]
  }
}
```

上面的对象作为装置的 `dataTests` 字段，表示每秒上报遥测 16385 的当前值，不改变点值。也可以分别保存 `yx`、`yc` 自动变化和 `yx-report`、`yc-report` 上报规则；未配置 `telemetry` 时服务器会合并这些分类。混用旧分类时建议先在桌面保存或执行 `validate` 检查。

| 规则字段             | 说明                                                                                             |
| -------------------- | ------------------------------------------------------------------------------------------------ |
| `enabled`            | 是否启用，默认 false                                                                             |
| `kind`               | 与目标点类型一致，如 `yx`、`yc`、`holding`                                                       |
| `points`             | 地址选择字符串，如 `"16385"`、`"16385-16390"`、`"16385,16387"`；只选择已有的同类型点，不会创建点 |
| `start`、`end`       | 不使用 `points` 时的含首尾地址范围；填写 `points` 时优先按它选择                                 |
| `intervalMs`         | 周期毫秒数，200–3600000                                                                          |
| `mode`               | 变化或上报方式，见下表                                                                           |
| `min`、`max`、`step` | 数值边界与递增步长；整数点用整数，递增步长必须大于 0                                             |
| `cause`              | 仅 `report` 使用：1 周期（仅遥测）、2 背景、3 突发；省略或 0 按 3 处理                           |

| mode        | 行为                                                          |
| ----------- | ------------------------------------------------------------- |
| `report`    | IEC104 定时发送当前值，不改变点值；保持点值不变时也会发送     |
| `fixed`     | 将值设置为 `min`                                              |
| `increment` | 每周期增加 `step`，超出 `max` 或当前值低于 `min` 时回到 `min` |
| `random`    | 在 `min`、`max` 范围取随机值，整数点保持整数                  |
| `toggle`    | 开关量每周期翻转；单点 0/1，双点 1/2                          |
| `sequence`  | 按地址排序，每周期只翻转所选开关量中的一个点，循环进行        |

例如让电压每秒从 220 增加到 230、步长 0.5，可将该点的规则替换为：

```json
{
  "enabled": true,
  "kind": "yc",
  "points": "16385",
  "intervalMs": 1000,
  "mode": "increment",
  "min": 220,
  "max": 230,
  "step": 0.5
}
```

每个规则配置最多 256 组。相同类型、同类行为（上报或变化）的启用规则不能重叠选点；同一点可分别配置变化规则和定时上报规则。规则的周期和数值范围需有效，类型与点表一致，报文品质取自点位；启动前执行 `prs validate`。

- **IEC104**：主站先连接并通过 STARTDT 激活链路，服务器才启动规则。自动变化会发送变化数据，`report` 按周期发送当前值。
- **Modbus**：规则修改内存中的点值，由主站用对应功能码轮询读取；不会主动推送报文，不能使用 `mode: report`。
- **断线策略**：`stopOnDisconnect: false` 时 IEC104 无已激活主站期间等待，重新激活后继续；设为 true 时任务断线停止，不会因未变更工程的热加载自动重启。重新启动实例或应用该通道的新配置后，按启动授权重新启动规则。

## 完整示例

### IEC104 子站：遥信翻转与遥测定时上报

保存为 `project.json`，或直接使用 [examples/server-iec104.json](examples/server-iec104.json)。监听 `127.0.0.1:24040`、公共单元地址 1；遥信地址 1 每两秒翻转，短浮点遥测 16385 每秒上报当前值 220.5。

```json
{
  "version": 2,
  "name": "IEC104服务器示例",
  "channels": [
    {
      "id": "iec104-server",
      "name": "IEC104子站",
      "protocol": "iec104",
      "role": "server",
      "transport": "tcp-server",
      "address": "127.0.0.1:24040",
      "timeoutMs": 3000,
      "devices": [
        {
          "id": "device-1",
          "name": "装置1",
          "unit": 1,
          "commonAddress": 1,
          "points": [
            {
              "address": 1,
              "kind": "yx",
              "wireType": 1,
              "name": "开关状态",
              "value": 0,
              "quality": 0
            },
            {
              "address": 16385,
              "kind": "yc",
              "wireType": 13,
              "name": "电压",
              "value": 220.5,
              "quality": 0
            }
          ],
          "dataTests": {
            "telemetry": {
              "stopOnDisconnect": false,
              "rules": [
                {
                  "enabled": true,
                  "kind": "yx",
                  "points": "1",
                  "intervalMs": 2000,
                  "mode": "toggle",
                  "min": 0,
                  "max": 1
                },
                {
                  "enabled": true,
                  "kind": "yc",
                  "points": "16385",
                  "intervalMs": 1000,
                  "mode": "report",
                  "cause": 1
                }
              ]
            }
          }
        }
      ]
    }
  ]
}
```

```bash
prs validate examples/server-iec104.json --json
prs run examples/server-iec104.json --start-data-rules --log-format json
```

### Modbus TCP 子站：保持寄存器自动递增

使用 [examples/server-modbus-tcp.json](examples/server-modbus-tcp.json)。监听 `127.0.0.1:15020`，单元地址 1；保持寄存器 0 每秒在 220–230 间递增，线圈 0 初始为 1。

```json
{
  "version": 2,
  "name": "Modbus服务器示例",
  "channels": [
    {
      "id": "modbus-server",
      "name": "Modbus子站",
      "protocol": "modbus-tcp",
      "role": "server",
      "transport": "tcp-server",
      "address": "127.0.0.1:15020",
      "timeoutMs": 3000,
      "devices": [
        {
          "id": "device-1",
          "name": "装置1",
          "unit": 1,
          "commonAddress": 1,
          "points": [
            {
              "address": 0,
              "kind": "holding",
              "dataType": "uint16",
              "byteOrder": "AB",
              "name": "测试电压",
              "value": 220,
              "quality": 0
            },
            {
              "address": 0,
              "kind": "coil",
              "name": "开关",
              "value": 1,
              "quality": 0
            }
          ],
          "dataTests": {
            "telemetry": {
              "stopOnDisconnect": false,
              "rules": [
                {
                  "enabled": true,
                  "kind": "holding",
                  "points": "0",
                  "intervalMs": 1000,
                  "mode": "increment",
                  "min": 220,
                  "max": 230,
                  "step": 1
                }
              ]
            }
          }
        }
      ]
    }
  ]
}
```

```bash
prs validate examples/server-modbus-tcp.json --json
prs run examples/server-modbus-tcp.json --start-data-rules --log-format json
```

主站读取单元地址 1、功能码 03、起始地址 0、数量 1，即可观察寄存器变化。示例绑定本机且端口不同，可以同时运行。

### 改成 Modbus RTU

将 Modbus 示例的通道 `protocol` 改为 `modbus-rtu`、`transport` 改为 `serial`、`address` 改为实际串口路径，再设置以下通道字段；装置及数据规则保持不变。

```json
{ "baud": 9600, "parity": "N", "dataBits": 8, "stopBits": 1, "timeoutMs": 3000 }
```

运行前用 `prs ports` 检查串口；服务用户需要设备访问权限。

## 热加载与多工程

`prs run` 默认监视工程路径。文件稳定约 0.5–1 秒后先校验再应用，支持直接修改、编辑器原子替换和目录中的符号链接更新。

- 只重启配置发生变化的通道；变更通道短暂断开，点值以新配置为准。新增通道按 `--channels` 选择启动，删除通道停止；显式选择的 ID 必须仍存在。
- 未变更通道保留连接、运行点值和任务。每个工程使用独立进程或 systemd 实例，修改一个工程不影响其他工程；不同实例需使用不同监听端点或独立串口。
- 无效 JSON、参数或通道选择会记录 `reload_rejected`，继续使用旧配置。新通道启动失败时尝试恢复旧配置，记录 `reload_rolled_back`；若旧端口或设备也无法恢复，服务退出，由 systemd 按策略重启。
- 修正配置后自动重新检测。若文件未变但想重试启动，可使用 `touch project.json`。
- 自动数据仍需 `--start-data-rules` 授权；变更通道的任务按新配置重新启动，IEC104 继续等待链路激活。未变更通道中因断线停止的任务不会被重新开启。

```bash
# 两个独立工程，各自监视自己的文件
prs run /etc/protocol-simulator/site-a.json --start-data-rules
prs run /etc/protocol-simulator/site-b.json --start-data-rules
```

以上分别在两个终端运行；常驻服务使用两个 systemd 实例，或按 [Docker 部署](#docker-部署) 使用独立容器。容器应只读挂载配置所在目录；单文件 bind mount 可能继续指向被原子替换前的旧文件，导致热加载无法识别新配置。

`--watch=false` 可关闭热加载。热加载只作用于工程 JSON；CLI 参数和 systemd 的 `.env` 变更需重启对应进程或实例。

## Docker 部署

镜像仅包含服务器命令 `prs`、示例和第三方许可，不包含桌面界面。Dockerfile 使用 Go 交叉编译，支持 `linux/amd64` 和 `linux/arm64`；版本仍取自 `wails.json`。镜像以 UID/GID `65532:65532` 运行，没有 shell，配置目录及工程文件需要允许该用户读取。

### 本地构建

在源码根目录使用 Docker Buildx 构建当前架构的镜像，并检查版本和示例配置。

```bash
docker buildx build --load -t protocol-simulator:local .
docker run --rm protocol-simulator:local version
docker run --rm protocol-simulator:local validate /examples/server-iec104.json --json
```

### 构建并上传 Docker Hub

目标仓库：[derorange/protocol-simulator](https://hub.docker.com/r/derorange/protocol-simulator)。先以具有该仓库写权限的账户登录；`docker login` 的认证在终端或浏览器完成。

```bash
docker login

# 创建构建器，仅首次执行；后续使用 docker buildx use prs-builder
docker buildx create --name prs-builder --driver docker-container --use
docker buildx inspect --bootstrap

VERSION="$(python3 -c 'import json; print(json.load(open("wails.json"))["info"]["productVersion"])')"
docker buildx build --platform linux/amd64,linux/arm64 \
  -t "derorange/protocol-simulator:$VERSION" \
  -t derorange/protocol-simulator:latest --push .

docker buildx imagetools inspect "derorange/protocol-simulator:$VERSION"
```

`--push` 会直接上传两种架构及 manifest；`imagetools inspect` 应显示 `linux/amd64`、`linux/arm64`。服务器拉取时自动选择匹配架构。正式部署建议固定版本标签；镜像构建、上传和远端检查是不同步骤，仓库创建完成不代表镜像已经发布。

### 挂载配置并运行

以下在部署服务器的新工作目录执行，需要 Docker 和 Python 3。将 `$VERSION` 替换为 GitHub Release 的版本号（去掉 v 前缀）；也可以使用本地构建的 `protocol-simulator:local`，跳过拉取。

```bash
VERSION=1.1.0
IMAGE="derorange/protocol-simulator:$VERSION"
docker pull "$IMAGE"
mkdir -p config

# 从镜像取出示例；已有工程时直接将自己的 JSON 放入 config/project.json
docker create --name prs-config-example "$IMAGE"
docker cp prs-config-example:/examples/server-iec104.json ./config/project.json
docker rm prs-config-example

# 示例原来只监听回环地址；端口映射要求容器内监听 0.0.0.0
python3 - <<'PY'
import json
from pathlib import Path
path = Path('config/project.json')
project = json.loads(path.read_text())
project['channels'][0]['address'] = '0.0.0.0:24040'
path.write_text(json.dumps(project, ensure_ascii=False, indent=2) + '\n')
PY
chmod 755 config
chmod 644 config/project.json

docker run -d --name prs --restart unless-stopped \
  --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --mount "type=bind,src=$PWD/config,dst=/config,readonly" \
  -p 24040:24040 \
  "$IMAGE" run /config/project.json --start-data-rules --log-format json
```

主站连接宿主机 IP 的 `24040` 端口。工程的监听端口与 `-p 宿主机端口:容器端口` 中的容器端口应一致；Modbus TCP 需按工程增加端口映射。RTU 则用 `--device /dev/ttyUSB0:/dev/ttyUSB0`，并按设备属组增加 `--group-add` 权限。

自动规则只有显式加上 `--start-data-rules` 才启动；不需要自动上报时去掉它。修改宿主机 `config/project.json` 后默认自动热加载，无需重建镜像。始终挂载整个目录，编辑器原子替换保存也能生效；修改一个容器的配置不影响其他容器。多工程分别使用独立配置目录、容器名和宿主机端口。

```bash
docker logs --tail 100 -f prs
docker exec -it prs prs tui
docker exec prs prs validate /config/project.json --json
docker stop --time 10 prs
docker start prs
```

`prs tui` 退出只关闭界面，容器内工程继续运行。运行日志输出到 Docker；`docker stop` 发送 SIGTERM，让程序正常停止。若启用 `--frames`，额外挂载可由 UID 65532 写入的日志目录并指定容器内路径。

### 无外网服务器：离线导入

在支持 `docker save --platform` 的联网 Docker 上导出对应架构；以下为 amd64，ARM 服务器改为 `linux/arm64`：

```bash
VERSION=1.1.0
IMAGE="derorange/protocol-simulator:$VERSION"
docker pull --platform linux/amd64 "$IMAGE"
docker image save --platform linux/amd64 \
  -o protocol-simulator-image.tar "$IMAGE"
gzip protocol-simulator-image.tar
shasum -a 256 protocol-simulator-image.tar.gz
```

将压缩包通过内网或文件传输工具复制到离线服务器，核对 SHA256 与导出端一致，再执行：

```bash
sha256sum protocol-simulator-image.tar.gz
docker load -i protocol-simulator-image.tar.gz
docker run --rm "$IMAGE" version
```

然后按前面的挂载配置步骤运行，跳过 `docker pull`。离线导入不需要 Docker Hub 登录，也不修改 Docker 服务的网络或镜像源配置。

## 日志

运行日志默认输出到标准输出，文本或 JSON 可选。原始报文默认不写磁盘，按需启用：

```bash
prs run project.json --log-format json \
  --frames /var/tmp/protocol-frames.jsonl --frames-max-mb 10 --frames-files 3
```

最多保留当前文件和 `.1`、`.2` 三个文件，每个不超过 10 MiB。报文 JSONL 记录完整时间戳、通道、收发方向、十六进制内容等。日志路径及轮转路径不得与工程文件重合，运行用户需要目录写入权限。

运行日志、报文日志都通过有界队列写出；积压时报告丢弃计数，因此不能将它当作保证每条报文落盘的抓包器。写入失败会结束服务，退出最多等待两秒刷新日志。systemd 将运行日志交给 journald；模板默认不配置报文文件路径。

通过终端界面或 `prs project new --start` 启动的独立工程，其运行日志保存在 `${XDG_STATE_HOME:-$HOME/.local/state}/protocol-simulator/server-logs/`。

## systemd 托管

用户服务与系统服务是两种安装方式，选择其中一种即可，不要让它们使用同一监听端口。以下以工程实例 `demo` 为例；名称对应 `demo.json`、`demo.env` 和 `protocol-simulator@demo.service`。

### 当前用户安装与用户服务

适用于 Ubuntu 24.04 等使用 systemd 的 Linux。以将来运行工程的普通用户通过 SSH 登录，不要在 `sudo su` 会话内执行 `systemctl --user`。程序、配置和服务安装不需要 root；无人登录时开机启动需要启用 linger，是否允许普通用户启用由系统策略决定。

默认安装路径如下。

| 内容         | 路径                                                                                |
| ------------ | ----------------------------------------------------------------------------------- |
| 命令入口     | `~/.local/bin/prs`                                                                  |
| 程序         | `~/.local/share/protocol-simulator/bin/prs`                                         |
| 工程配置     | `~/.config/protocol-simulator/`                                                     |
| 日志和报文   | `~/.local/state/protocol-simulator/`，用户服务运行日志可用 `journalctl --user` 查看 |
| 用户服务模板 | `~/.config/systemd/user/protocol-simulator@.service`                                |
| 手册         | `~/.local/share/man/man1/prs.1`                                                     |

#### 1. 解压、安装程序和配置

将对应架构的服务器压缩包上传到服务器，在压缩包所在目录完整执行下面代码。用于首次安装：若已有程序或 `prs` 命令链接，停止并检查已有安装，升级参见后文；不会覆盖已有工程 JSON。

```bash
(
set -eu
case "$(uname -m)" in
  x86_64) prs_arch=amd64 ;;
  aarch64|arm64) prs_arch=arm64 ;;
  *) echo "不支持的 CPU 架构" >&2; exit 1 ;;
esac

test ! -e "$HOME/.local/share/protocol-simulator/bin/prs"
test ! -e "$HOME/.local/bin/prs"
test ! -L "$HOME/.local/bin/prs"
tar -xzf "protocol-simulator-server-linux-${prs_arch}.tar.gz"
cd protocol-simulator-server
install -d -m 0755 "$HOME/.local/bin" "$HOME/.local/share/protocol-simulator/bin"
install -d -m 0700 "$HOME/.config/protocol-simulator" "$HOME/.local/state/protocol-simulator"
install -m 0755 bin/prs "$HOME/.local/share/protocol-simulator/bin/prs"
ln -s "$HOME/.local/share/protocol-simulator/bin/prs" "$HOME/.local/bin/prs"
install -D -m 0644 share/man/man1/prs.1 "$HOME/.local/share/man/man1/prs.1"
if [ ! -e "$HOME/.config/protocol-simulator/demo.json" ]; then
  install -m 0600 examples/server-iec104.json "$HOME/.config/protocol-simulator/demo.json"
fi
"$HOME/.local/bin/prs" version
"$HOME/.local/bin/prs" validate "$HOME/.config/protocol-simulator/demo.json" --json
)
```

默认创建可运行的 IEC104 示例，监听 `127.0.0.1:24040`。需要迁移真实工程时，在启动服务前用自己的 JSON 替换 `~/.config/protocol-simulator/demo.json`，并再次校验。远程设备接入时将工程的监听地址改为服务器明确的网卡地址，放行对应 TCP 端口；主站填写同一个服务器 IP。点位与自动规则来自 JSON，不会从旧服务器自动同步。

下面设置当前终端的命令路径；后续代码即使没有设置 PATH，也会通过绝对路径运行：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

若新登录后 `command -v prs` 没有结果，将上面一行加入当前 shell 的启动文件（Bash 通常是 `~/.profile`，Zsh 是 `~/.zprofile`），然后重新登录。安装了 `man` 的环境可执行 `man prs`，手册位于 `~/.local/share/man/man1/prs.1`。

#### 2. 创建 systemd 用户服务

以下生成用户专用模板。包内 `systemd/protocol-simulator@.service` 是系统级模板，不能直接复制作为用户服务。已有同名用户模板时先核对，避免覆盖自定义参数。

```bash
(
set -eu
install -d -m 0700 "$HOME/.config/systemd/user"
test ! -e "$HOME/.config/systemd/user/protocol-simulator@.service"
cat > "$HOME/.config/systemd/user/protocol-simulator@.service" <<'EOF'
[Unit]
Description=Protocol Simulator (%i)
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=exec
Environment=CHANNELS=all
Environment=START_DATA_RULES=false
Environment=WATCH=true
EnvironmentFile=-%h/.config/protocol-simulator/%i.env
RuntimeDirectory=protocol-simulator
RuntimeDirectoryMode=0700
RuntimeDirectoryPreserve=yes
ExecStart="%h/.local/share/protocol-simulator/bin/prs" run "%h/.config/protocol-simulator/%i.json" --channels ${CHANNELS} --start-data-rules=${START_DATA_RULES} --watch=${WATCH} --runtime-dir %t/protocol-simulator --log-format json --frames "%h/.local/state/protocol-simulator/%i.frames.jsonl" --frames-max-mb 10 --frames-files 3
Restart=on-failure
RestartPreventExitStatus=2
RestartSec=3
TimeoutStopSec=15
NoNewPrivileges=true
UMask=0077
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
EOF
chmod 0600 "$HOME/.config/systemd/user/protocol-simulator@.service"
systemd-analyze --user verify "$HOME/.config/systemd/user/protocol-simulator@.service"
systemctl --user daemon-reload
)
```

模板中的 `%h`、`%t`、`%i` 由 systemd 展开；`${CHANNELS}` 等变量由服务环境展开，不需要手工替换。各工程的报文日志最多保留 3 个、每个 10 MiB；运行日志进入用户 journal。共享 socket 目录为 `$XDG_RUNTIME_DIR/protocol-simulator`，仅当前用户可访问，停止一个实例不影响其他实例。

#### 3. 启动工程、查看状态与界面

下面显式开启 JSON 中已保存、已启用的自动数据/上报规则；不希望启动这些规则时，将 `START_DATA_RULES=true` 改为 `false`。不会启动自动遥控、自动遥调任务。已有 `.env` 时保留其内容，请自行编辑。

```bash
(
set -eu
if [ ! -e "$HOME/.config/protocol-simulator/demo.env" ]; then
  (umask 077; cat > "$HOME/.config/protocol-simulator/demo.env" <<'EOF'
CHANNELS=all
START_DATA_RULES=true
WATCH=true
EOF
  )
fi
"$HOME/.local/bin/prs" validate "$HOME/.config/protocol-simulator/demo.json" --json
systemctl --user enable --now protocol-simulator@demo.service
systemctl --user is-enabled protocol-simulator@demo.service
systemctl --user is-active protocol-simulator@demo.service
systemctl --user status protocol-simulator@demo.service --no-pager
)
```

在同一用户的 SSH 会话执行，不需要 `sudo`：

```bash
"$HOME/.local/bin/prs" tui
journalctl --user -u protocol-simulator@demo.service -n 100 --no-pager
journalctl --user -u protocol-simulator@demo.service -f
```

以上三条分别执行；`tui` 用 `q` 退出，日志跟随用 Ctrl+C 结束，都不会停止工程。JSON 热加载默认开启；修改 `.env` 后需要 `systemctl --user restart protocol-simulator@demo.service`。

#### 4. 退出 SSH 后继续运行、无人登录时开机自启

`enable --now` 负责启用服务，还需要 linger 才能在无人登录时随开机启动，并在退出 SSH 后保留用户管理器（[systemd 官方说明](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html#enable-linger%20%5BUSER%E2%80%A6%5D)）。先以当前普通用户执行：

```bash
loginctl enable-linger "$(id -un)"
loginctl show-user "$(id -un)" -p Linger --value
systemctl --user is-enabled protocol-simulator@demo.service
systemctl --user is-active protocol-simulator@demo.service
```

期望依次看到 `yes`、`enabled`、`active`。若第一条被系统策略拒绝，需要管理员执行下面这一条；其中用户名由当前普通用户的 shell 展开，不要切换到 root 后再取用户名：

```bash
sudo loginctl enable-linger "$(id -un)"
```

没有 sudo 权限时，由管理员为你的账号启用 linger；无法启用时，不能保证退出登录后持续运行或无人登录开机启动。不要把 `nohup`、`&` 或仅 `enable` 当作完整替代。启用后可退出 SSH 再登录核对状态；服务器下次正常重启后，用下面两条核对启动结果，无需为安装主动重启整台服务器：

```bash
systemctl --user is-active protocol-simulator@demo.service
journalctl --user -u protocol-simulator@demo.service -b -n 100 --no-pager
```

若 `systemctl --user` 提示无法连接总线，重新用目标用户直接 SSH 登录，并检查 `echo "$XDG_RUNTIME_DIR"` 与 `loginctl user-status "$(id -un)"`；不要给其他用户的 runtime 目录改权限，也不要用 `sudo systemctl --user`。精简安装的 Ubuntu / Debian 若缺少用户会话组件，由管理员执行 `sudo apt install libpam-systemd dbus-user-session` 后重新登录。

#### 5. 多工程、升级与卸载

第二个工程使用独立文件和实例，监听端口或串口必须与其他实例不同：

```bash
"$HOME/.local/bin/prs" validate "$HOME/.config/protocol-simulator/site-b.json" --json
systemctl --user enable --now protocol-simulator@site-b.service
```

请先准备 `site-b.json`，需要自动规则时另建 `site-b.env`；不要直接复制示例后使用相同监听端口。每个实例单独启停：

```bash
systemctl --user stop protocol-simulator@demo.service
systemctl --user start protocol-simulator@demo.service
systemctl --user restart protocol-simulator@demo.service
systemctl --user disable --now protocol-simulator@demo.service
```

升级时先解压新包并进入其 `protocol-simulator-server/` 目录。共享程序的所有运行实例都需要停下再替换；以下仅展示一个实例，有其他实例时逐一停止并在替换后逐一启动：

```bash
(
set -eu
systemctl --user stop protocol-simulator@demo.service
install -m 0755 bin/prs "$HOME/.local/share/protocol-simulator/bin/prs.new"
mv -f "$HOME/.local/share/protocol-simulator/bin/prs.new" "$HOME/.local/share/protocol-simulator/bin/prs"
install -D -m 0644 share/man/man1/prs.1 "$HOME/.local/share/man/man1/prs.1"
"$HOME/.local/bin/prs" version
"$HOME/.local/bin/prs" validate "$HOME/.config/protocol-simulator/demo.json" --json
systemctl --user start protocol-simulator@demo.service
systemctl --user is-active protocol-simulator@demo.service
)
```

升级不覆盖 JSON、`.env` 和日志。卸载时先对所有实例执行 `disable --now`；仅在没有其他实例使用时执行下面的程序文件清理，配置和日志继续保留：

```bash
rm "$HOME/.config/systemd/user/protocol-simulator@.service"
systemctl --user daemon-reload
if [ -L "$HOME/.local/bin/prs" ] && [ "$(readlink "$HOME/.local/bin/prs")" = "$HOME/.local/share/protocol-simulator/bin/prs" ]; then
  rm "$HOME/.local/bin/prs"
fi
rm "$HOME/.local/share/protocol-simulator/bin/prs"
rm -f "$HOME/.local/share/man/man1/prs.1"
```

不要自动关闭 linger，它可能也供该用户的其他服务使用。

### 系统级安装与服务

#### 首次安装

在服务器包的解压目录执行。`project.json` 是已准备好的工程文件，例如从 `examples/server-iec104.json` 复制并修改。安装步骤本身不会启动通信。

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin protocol-simulator
sudo install -D -m 0755 bin/prs /opt/protocol-simulator/bin/prs
sudo ln -s /opt/protocol-simulator/bin/prs /usr/local/bin/prs
sudo install -D -m 0644 share/man/man1/prs.1 /usr/local/share/man/man1/prs.1
sudo install -D -m 0644 systemd/protocol-simulator@.service /etc/systemd/system/protocol-simulator@.service
sudo install -d -m 0750 -o root -g protocol-simulator /etc/protocol-simulator
sudo install -m 0640 -o root -g protocol-simulator project.json /etc/protocol-simulator/demo.json
sudo systemctl daemon-reload
```

`prs` 命令链接为可选项；若同名路径已存在，`ln` 会拒绝覆盖。服务使用 `/opt/protocol-simulator/bin/prs` 绝对路径。已有专用用户时跳过 `useradd`，不要修改其他账户。

#### 每个工程的配置

实例 `protocol-simulator@demo` 对应 `/etc/protocol-simulator/demo.json`。可选的环境文件为 `/etc/protocol-simulator/demo.env`：

```text
CHANNELS=all
START_DATA_RULES=true
WATCH=true
```

| 变量               | 模板默认值 | 含义                          |
| ------------------ | ---------- | ----------------------------- |
| `CHANNELS`         | `all`      | 全部或逗号分隔的通道 ID       |
| `START_DATA_RULES` | `false`    | 是否授权启动自动数据/上报规则 |
| `WATCH`            | `true`     | 是否热加载工程 JSON           |

示例环境文件显式开启了数据规则。由管理员保存为 root 所有、`protocol-simulator` 组可读的文件，例如 `0640`。修改 `.env` 后重启该实例；修改 JSON 不需要重启。

#### 启停、状态与日志

```bash
sudo systemctl start protocol-simulator@demo
sudo systemctl stop protocol-simulator@demo
sudo systemctl restart protocol-simulator@demo
sudo systemctl status protocol-simulator@demo
sudo systemctl enable --now protocol-simulator@demo
sudo systemctl disable protocol-simulator@demo
sudo journalctl -u protocol-simulator@demo -f
sudo journalctl -u protocol-simulator@demo -n 100 --no-pager
```

`enable --now` 设置开机启动并立即运行；`disable` 仅取消开机启动，不停止当前服务，需同时停止时使用 `disable --now`。另一个工程保存为 `site-b.json`，启动 `protocol-simulator@site-b`，各自独立管理。

服务使用专用用户运行，失败后按模板策略重启，参数或工程预检错误（退出码 2）不会反复重启。串口需配置设备所属组或 udev 权限。监听受限低端口时可通过 `systemctl edit protocol-simulator@demo` 给指定实例增加 `AmbientCapabilities=CAP_NET_BIND_SERVICE` 和 `CapabilityBoundingSet=CAP_NET_BIND_SERVICE`，无需把服务改成 root。

升级程序前停止相关实例，替换 `/opt/protocol-simulator/bin/prs` 后再启动。卸载时先停止并禁用实例，保留 `/etc/protocol-simulator/` 内的用户工程及 `.env`；仅移除已安装的程序、命令链接和 unit 文件，再执行 `systemctl daemon-reload`。

## 支持范围与开发

支持标准 IEC104、Modbus TCP/RTU。厂商私有 SOE、虚遥信、五防闭锁等扩展不在完整兼容范围内。服务器通过所属用户专用的 Unix socket 提供观测和明确的数据点操作，不提供 TCP 远程管理 API；点表与规则配置仍通过工程文件或桌面版管理。

源码使用 Go + Wails 和原生 HTML/CSS/JavaScript。修改界面时运行 `npm run dev`，主窗口和配置窗口自动刷新；CSS 保留当前输入，HTML/JS 刷新会重新初始化界面，Go 修改会自动重启开发进程。构建、测试及平台验证边界见 [CONTRIBUTING.md](https://github.com/Masterora/protocol-simulator/blob/main/CONTRIBUTING.md)。Linux 桌面验收覆盖 X11，Wayland 需另行验证；串口 PTY 验证不代替物理 RS-485。Windows 交叉构建和包校验不代表 Windows 实机安装验收。
