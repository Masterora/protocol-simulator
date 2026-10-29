# protocol-simulator

IEC104、Modbus TCP/RTU 规约调试与模拟工具。桌面版提供点表、四遥操作、自动任务和报文界面；Linux 服务器版 `prs` 通过 JSON 工程运行，支持配置热加载、日志和 systemd。也支持 RAW TCP、UDP 和串口收发。

- [安装](#安装)
- [桌面版](#桌面版)
- [服务器版 prs](#服务器版-prs)
- [工程 JSON 与点位](#工程-json-与点位)
- [自动数据与上报规则](#自动数据与上报规则)
- [完整示例](#完整示例)
- [热加载与多工程](#热加载与多工程)
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
- **Linux 桌面**：运行 `sudo apt install ./protocol-simulator-desktop-linux-amd64.deb`，ARM64 改用 arm64 包，再从应用菜单启动。
- **Linux 服务器**：按下文解压，使用 `./bin/prs`；安装命令链接后可直接输入 `prs`，不影响系统 `ps`。

## 桌面版

### 快速开始：模拟 IEC104 子站

1. 启动后在顶部切换到“子站”。打开“通道设置”，选择 TCP 服务端，填写监听 IP 和端口。
2. 点击装置列表右侧的 **＋**，选择 IEC104，填写公共单元地址和四遥点位。遥测默认使用短浮点。
3. 在遥信或遥测页打开“自动上报配置”，点击 **＋** 添加规则，填写类型、点位、周期与方式，保存。新工程和新装置默认没有规则。
4. 点击“连接”启动子站监听，让主站按相同的 IP、端口、公共单元地址和点位类型接入，并激活 IEC104 链路。
5. 在主界面点击“开启自动上报”，在主站核对对应点位的数据，再保存工程以便下次使用。

配置窗口只保存参数，任务从主界面手动启动。IEC104 需要完成链路激活；只有 TCP 连接不代表可以上报数据。

无需外部主站时，可打开[本机四遥联调示例](examples/automatic-loopback.json)：先在子站连接示例服务端，再切换到主站连接示例客户端。载入工程不会自动连接或启动任务。

### 常用操作

- **遥控、遥调**：IEC104 主站支持选择、执行、撤销、直控和自动任务；子站可将遥控绑定到遥信，将遥调绑定到遥测，执行后修改绑定值并反馈。选择、撤销、否认和不应答不会改变绑定值。
- **Modbus 数据**：主站读取线圈、离散量和寄存器，写入线圈或保持寄存器；子站的“自动数据配置”修改点值，由主站轮询读取。
- **点表**：单击单元格编辑，Enter 提交，Esc 取消。搜索匹配点位或描述，每页最多 100 条；批量点位可写成 `1，3-5，8`。
- **通信**：有效的配置更改会断开对应通道，之后手动重连。在线修改点值、品质或批量设值保持连接；主站本地点值编辑不发送协议帧。
- **报文**：按连接地址、方向和通道筛选，可导出 CSV。暂停显示不会停止通信。

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

示例默认监听本机；远程主站接入时将工程的监听地址改为可回连的网卡地址，或 `0.0.0.0:24040`。主站连接的是服务器实际 IP。

`run` 默认启动全部通道并开启热加载，自动数据规则默认不启动，需显式加上 `--start-data-rules`。工程文件只读，运行点值不会写回 JSON。前台按 Ctrl+C 或发送 SIGTERM 正常退出并释放连接、端口和串口。TCP 客户端在未连接时持续等待和重试，直到停止。

### 全部命令

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

`validate` 和 `run` 也可写成 `prs validate --project project.json`、`prs run --project project.json`。一个命令只提供一个工程路径，不要同时使用位置参数和 `--project`。推荐将文件路径放在子命令后、其他参数前。

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

界面只连接已有进程，不会重复启动工程。启动后自动选择有效工程、通道和装置；直接按数字键或输入资源命令即可查看，切换工程时使用工程列表中的高亮行。每个工程使用独立 Unix socket；退出、Ctrl+C、界面崩溃或观测连接中断都不会停止工程通信。支持通过 SSH 使用，终端至少 60 列、12 行。

| 按键                                                                                | 操作                                                                                 |
| ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `1`–`7`、Tab                                                                        | 工程、通道、装置、点位、规则、报文、日志视图                                         |
| Enter                                                                               | 进入工程 → 通道 → 装置 → 点位；点位、规则、报文、日志打开详情                        |
| `v`                                                                                 | 任意列表查看完整详情；报文显示带偏移的十六进制转储                                   |
| `f` / `l`                                                                           | 直接查看当前通道报文 / 工程日志                                                      |
| ↑/↓、g/G                                                                            | 移动选择、到首行或末行                                                               |
| ←/→                                                                                 | 切换点位分类；切换后清除筛选并回到该类第一页                                         |
| `/` 输入文字后 Enter                                                                | 搜索当前视图，支持中文与大小写不敏感匹配                                             |
| Esc                                                                                 | 取消输入、关闭详情、清除搜索或返回上层                                               |
| PgUp/PgDn、`[`/`]`                                                                  | 点位按分类分页，每页 100 条；其他列表翻页                                            |
| `e` / `s`                                                                           | 点位页修改数值、品质 / 立即上报；修改表单用 Tab 或 ↑↓ 切换字段，Enter 应用，Esc 取消 |
| `p` / 空格                                                                          | 暂停或恢复界面刷新，工程持续运行                                                     |
| `?` / Ctrl+A                                                                        | 快捷键与资源别名帮助，支持上下滚动                                                   |
| `:projects` / `:channels` / `:devices` / `:points` / `:rules` / `:frames` / `:logs` | 切换视图                                                                             |
| `q`、Ctrl+C、`:q`                                                                   | 退出界面，保留工程运行                                                               |

资源命令支持中文名称，例如 `:工程`、`:通道`、`:装置`、`:点位`、`:规则`、`:报文`、`:日志`；原有英文命令和简写继续兼容。资源命令支持简写：`:prj`、`:ch`、`:dev`、`:pt`、`:r`、`:f`、`:l`。报文与日志默认跟随最新记录，按上下方向键停止跟随，按 `G` 恢复。详情窗按所选记录的标识持续刷新并保持滚动位置，Esc 返回列表。

界面每 250 毫秒尝试从运行进程读取一次最新状态；请求延迟可能延长实际刷新间隔。状态栏显示最近一次成功刷新时间。自动生成、手动修改和协议接收产生的新值都随下一次刷新显示；界面不等待自动规则周期。自动规则周期仅决定何时产生下一次自动变化，手动操作不会暂停或重置该规则。`p` / 空格暂停显示时，数值和界面更新时间保持不变，后台通信和自动上报继续；恢复后读取最新状态。暂停和恢复同时作用于列表与详情窗。点位、品质、通道状态和规则执行次数在详情中实时变化；报文、日志按记录编号保持选中，历史记录被淘汰时明确提示，不自动切换到另一条记录。

点位视图中，遥信、遥测、遥控、遥调独立显示，按左右方向键切换；Modbus 的保持寄存器、输入寄存器、线圈、离散输入也分别显示。各分类按地址数值升序排列，先分类和排序再分页，搜索仅作用于当前分类。顶部显示分类数量，搜索时另显示匹配数量；分页仅在表格底部显示。当前页面与分类用颜色强调；底部显示快捷键提示、实时状态、历史缺口和最近更新时间。点位列表显示实时值和品质，规则视图显示配置、状态和执行次数；报文视图显示 RX/TX 十六进制数据，Enter 可展开长报文；日志视图显示运行事件、协议提示及热加载结果。历史有界，界面保留最近 1000 条记录；暂停或断开过久时可能跳过旧记录，界面会显示服务端历史缺口计数。

工程默认在 `PRS_RUNTIME_DIR` 指定目录创建 socket；未设置时使用 `$XDG_RUNTIME_DIR/protocol-simulator`，再回退到系统临时目录下的 `protocol-simulator-UID`。默认目录及 socket 仅所属用户可访问；自定义目录不得为符号链接，也不得允许组或其他用户写入。两端可以同时用 `--runtime-dir DIR` 指向同一目录。界面只允许明确触发的数据点修改和立即上报，不提供通道或规则启停；配置继续通过 JSON 热加载管理。运行中的旧版本仍保持只读，更新其服务进程后才能使用新操作。

systemd 模板使用 `/run/protocol-simulator`，与 PrivateTmp 隔离无关。查看其工程时使用有权限的用户，例如：

```bash
sudo /opt/protocol-simulator/bin/prs tui --runtime-dir /run/protocol-simulator
```

模板会创建并保留共享的专用运行目录；停止某个实例只清理自己的 socket，其他实例继续运行。若界面没有列出工程，检查使用的用户、目录和工程日志中的 `observer_ready` / `observer_unavailable`。旧版本运行中的进程需要升级并重启后才会提供观测接口。

### 手动修改与立即上报

在点位页选中遥信、遥测或 Modbus 数据点，按 `e` 编辑数值和品质；输入会替换当前字段，Tab / ↑↓ 切换字段，Ctrl+U 清空，Enter 应用，Esc 取消。仅提交实际编辑的字段，例如只改品质时保留后台最新数值。IEC104 子站在连接激活后立即上报；未连接时明确显示“已修改，未上报”。Modbus 不主动发报，主站下次读取时得到新值。

在 IEC104 子站的数据点上按 `s`，立即上报该点此刻的最新数值和品质，不改变点值，也不广播整个装置。服务器入口不下发遥控、遥调；对应点位不能通过此入口修改或发送。

自动规则保持运行。自动、手动和协议更新按实际发生顺序应用，列表和详情持续显示最新值；手动值随后可能被自动规则更新。手动操作只改变运行状态，不写回 JSON，不影响其他工程。

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

`--value` 和 `--quality` 至少提供一项；品质为 0–255，地址支持十进制和 `0x` 十六进制。可用 `--runtime-dir DIR` 指定观测目录。命令自动读取当前配置版本；配置变更、目标消失或不合法数值会被拒绝。退出码 `2` 表示参数错误，`1` 表示操作失败；断网或上报部分失败时不会自动重试，请先核对最新状态。JSON 结果中的 `applied` 表示运行值是否已修改，`receivers` 是实际上报成功的连接数，`mode` 区分 `reported`、`offline` 和 `polled`。

## 工程 JSON 与点位

可以在桌面版完成配置后保存为 JSON，也可以按下面格式编辑。新配置使用 `version: 2` 和 `devices` 列表；兼容读取旧版 `version: 1`。JSON 不支持注释或尾逗号，未知字段会被拒绝。完整例子见 [完整示例](#完整示例)。

### 工程、通道与装置

| 层级 | 字段                                     | 说明                                                                                          |
| ---- | ---------------------------------------- | --------------------------------------------------------------------------------------------- |
| 工程 | `version`                                | 格式版本，新配置填 `2`                                                                        |
| 工程 | `name`                                   | 非空工程名称                                                                                  |
| 工程 | `channels`                               | 通道数组，最多 64 个；默认选择全部通道时，空工程保持等待，之后可通过热加载添加通道            |
| 通道 | `id`、`name`                             | 稳定且在工程内唯一的 ID、非空显示名称；修改时保留 ID                                          |
| 通道 | `protocol`                               | `iec104`、`modbus-tcp`、`modbus-rtu`、`raw`                                                   |
| 通道 | `role`                                   | `server` 子站或 `client` 主站；与传输连接方向分别设置                                         |
| 通道 | `transport`                              | `tcp-server`、`tcp-client`、`udp`、`serial`，组合限制见下表                                   |
| 通道 | `address`                                | 网络端点，如 `127.0.0.1:24040`、`[::1]:24040`；串口则填 `/dev/ttyUSB0` 等设备路径             |
| 通道 | `localAddress`                           | TCP 客户端或 UDP 的可选本地绑定地址，含端口；留空由系统选择                                   |
| 通道 | `timeoutMs`                              | 超时毫秒数，默认 `3000`，范围 100–60000                                                       |
| 通道 | `baud`、`parity`、`dataBits`、`stopBits` | 串口参数，默认 `9600`、`N`、`8`、`1`；波特率 50–4000000，校验 `N/E/O`，数据位 5–8，停止位 1/2 |
| 通道 | `devices`                                | 装置列表，最多 256 个；IEC104 每通道一个装置，RAW 使用空数组                                  |
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
| `raw`                  | `tcp-server` / `tcp-client` / `udp` / `serial`          |

TCP 服务端 `address` 是本地监听端点；TCP 客户端或 UDP 的 `address` 是对端。网络地址必须包含主机和端口，串口运行前用 `prs ports` 确认设备及权限。

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

### 改成 Modbus RTU 或 RAW

将 Modbus 示例的通道 `protocol` 改为 `modbus-rtu`、`transport` 改为 `serial`、`address` 改为实际串口路径，再设置以下通道字段；装置及数据规则保持不变。

```json
{ "baud": 9600, "parity": "N", "dataBits": 8, "stopBits": 1, "timeoutMs": 3000 }
```

运行前用 `prs ports` 检查串口；服务用户需要设备访问权限。RAW 通道使用 `protocol: "raw"`、`devices: []`，按实际连接选择传输类型，不设置点表或自动数据规则。

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

以上分别在两个终端运行；常驻服务使用两个 systemd 实例。容器部署建议将配置所在目录只读挂载给程序，再传入容器内的工程路径；单文件 bind mount 可能继续指向被原子替换前的旧文件。当前交付为独立二进制压缩包，不包含预构建容器镜像。

`--watch=false` 可关闭热加载。热加载只作用于工程 JSON；CLI 参数和 systemd 的 `.env` 变更需重启对应进程或实例。

## 日志

运行日志默认输出到标准输出，文本或 JSON 可选。原始报文默认不写磁盘，按需启用：

```bash
prs run project.json --log-format json \
  --frames /var/tmp/protocol-frames.jsonl --frames-max-mb 10 --frames-files 3
```

最多保留当前文件和 `.1`、`.2` 三个文件，每个不超过 10 MiB。报文 JSONL 记录完整时间戳、通道、收发方向、十六进制内容等。日志路径及轮转路径不得与工程文件重合，运行用户需要目录写入权限。

运行日志、报文日志都通过有界队列写出；积压时报告丢弃计数，因此不能将它当作保证每条报文落盘的抓包器。写入失败会结束服务，退出最多等待两秒刷新日志。systemd 将运行日志交给 journald；模板默认不配置报文文件路径。

## systemd 托管

### 首次安装

在服务器包的解压目录执行。`project.json` 是已准备好的工程文件，例如从 `examples/server-iec104.json` 复制并修改。安装步骤本身不会启动通信。

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin protocol-simulator
sudo install -D -m 0755 bin/prs /opt/protocol-simulator/bin/prs
sudo ln -s /opt/protocol-simulator/bin/prs /usr/local/bin/prs
sudo install -D -m 0644 systemd/protocol-simulator@.service /etc/systemd/system/protocol-simulator@.service
sudo install -d -m 0750 -o root -g protocol-simulator /etc/protocol-simulator
sudo install -m 0640 -o root -g protocol-simulator project.json /etc/protocol-simulator/demo.json
sudo systemctl daemon-reload
```

`prs` 命令链接为可选项；若同名路径已存在，`ln` 会拒绝覆盖。服务使用 `/opt/protocol-simulator/bin/prs` 绝对路径。已有专用用户时跳过 `useradd`，不要修改其他账户。

### 每个工程的配置

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

### 启停、状态与日志

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

支持标准 IEC104、Modbus TCP/RTU、RAW TCP、UDP 和串口。厂商私有 SOE、虚遥信、五防闭锁等扩展不在完整兼容范围内。服务器通过所属用户专用的 Unix socket 提供观测和明确的数据点操作，不提供 TCP 远程管理 API；点表与规则配置仍通过工程文件或桌面版管理。

源码使用 Go + Wails 和原生 HTML/CSS/JavaScript。构建、测试及平台验证边界见 [CONTRIBUTING.md](https://github.com/Masterora/protocol-simulator/blob/main/CONTRIBUTING.md)。Linux 桌面验收覆盖 X11，Wayland 需另行验证；串口 PTY 验证不代替物理 RS-485。Windows 交叉构建和包校验不代表 Windows 实机安装验收。
