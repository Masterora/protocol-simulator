## protocol-simulator delivery

- The app product name is `protocol-simulator`; executable name is `protocol-simulator.exe`.
- Keep the dark theme and original GYTest layout and interactions.
- User override: communication IP and port fields remain editable while connected. Applying changed communication settings stops an active channel, saves and closes the editor; reconnect only when the user manually connects. Stopped channels stay stopped. Validate before disconnecting and preserve device/point data. Communication manager edits channel names and communication parameters inline, without edit buttons or opening another editor. Enter/blur commits once, Escape cancels, polling preserves drafts, and queued edits use the latest configuration. All input and select fields have no native up/down or dropdown arrow decorations, including datalist fields. Numeric inputs have no spinner arrows or arrow-key increments.
- Canonical development source: the current repository root. Publish only `outputs/protocol-simulator.dmg` and `outputs/protocol-simulator-Setup.exe`; no ZIP or public unpacked application folder. Use temporary staging under `work/`. Setup installs per user, checks/installs WebView2, registers shortcuts and uninstall, refuses to modify a running app, and preserves projects and editable examples. Verify installer payload/resources and the mounted read-only DMG/copied app signature; report Windows runtime acceptance separately. When retiring legacy delivery folders, remove only manifest-matching generated files and preserve user edits at their original paths.
- After each completed modification, build and verify the executable, embed `build/windows/icon.ico` using `build/windows/resources.ps1`, then replace the current delivery and remove superseded generated application folders, archives and executables. Do not accumulate versioned releases.
- Preserve original GYTest installations, reverse engineering references, user projects and configurations. Verify absolute paths before recursive deletion. Never delete working source during packaging.
- Use `scripts/package_protocol.py` to produce and verify the current delivery. Keep source and frontend synchronized with the executable.

- User overrides: TCP clients wait and automatically retry until stopped; successful device creation closes its window; closing a project main window exits its owned editor windows and background tasks without confirmation, while independent project windows remain running.

- Startup opens the client workspace directly with a default stopped channel. There is no mode-picker window or home navigation; switch client/server using the top menu, preserving window geometry.

- Server telemetry values are editable inline: Enter or blur commits once; Escape cancels; polling preserves active drafts. IEC104 reports only the edited YX/YC point with its existing metadata; offline edits explicitly indicate they were not sent. Modbus edits update the polled data without unsolicited frames. Client point values are editable locally and must never send protocol frames.

- Keep the established dark blue/grey theme; improve readability by brightening text, labels, headers and input values rather than changing the theme. Keep readable text contrast and clear keyboard focus. Keep the message display pause/resume button visible without stopping communication.

- New device protocol selection defaults to IEC104 (standard version); preserve explicitly selected protocols and existing device configurations. New device point-count fields default to 50 (previously 18); preserve configured/imported counts. Place the channel list above the device list in the left sidebar, with equal-height independently scrolling sections.

- The left sidebar width and channel/device height ratio are draggable and remembered, with a default 50/50 height split; double-click or Home resets the corresponding separator. Native popups must not overwrite these preferences. The point grid has five editable columns (address, hex, value, quality, description), no operations column; detailed configuration and deletion are in the row context menu. Enter/blur commits once, Escape cancels, and queued cell edits follow a successfully changed address.

- Channel-name context menu contains only 重命名, 复制, 删除, in this order; no remove-all or batch-add actions.

- Add channel/device from + buttons at the right of their sidebar captions; no top ribbon channel-configuration group. Creation windows expose a name with an unused default (通道N / 装置N).

- Default workspace channel is 通道1; new channel names start at 1, preserving existing imported names. Sidebar caption is 装置. Remove ribbon group captions 通信配置 / 数据备份 while keeping their buttons; label the save action 保存.

- Main status bar bottom-right shows Produced by Der_Orange with an orange emoji 🍊 immediately to its right.

- Non-editable UI text cannot be selected, including labels, headers, messages, status and attribution. Enabled editable fields retain normal text selection, copy and editing.

- Creating a new project opens an independent main-window process with its own engine, saved path and editor service, initializing one stopped 通道1 for the current client/server mode. The original project, connections and tasks remain untouched.

- Startup project name is 工程1. New-project names default to 工程 plus an increasing session number, beginning with 工程2 after startup; cancellation does not consume a number. Custom and loaded names are preserved.

- Save and Save As dialogs suggest the current project name with a .json extension (for example 工程1.json). Normal Save on an existing path continues writing that path.

- Successful Save / Save As synchronizes the in-memory and persisted project name to the chosen filename without its .json suffix. Cancelled or failed saves preserve name, path and dirty state.

- The upper-right project label appends （未保存） when the project has no saved path or has unsaved edits; successful save clears it. Connection status and received messages alone do not mark it dirty. Native main-window titles include the project name to distinguish independent projects.

- Resize the debug panel using an external horizontal separator between messages and debug output, not a native in-panel resize grip. Remember its height; double-click or Home restores 130px. Native popups do not overwrite it. Point badges show current-list point count (for example 50 点), never a misleading count/total fraction; total points remain available in the tooltip.

- Upper-right header displays only the project name and optional （未保存） marker. Do not show a duplicate filename beside it; keep the full saved path in the project-name tooltip.

- New projects and devices start without automatic reporting/data rules. Opening or saving an unchanged empty rule editor must not generate rules or dirty the project. Add rules explicitly using a + symbol button with an accessible label; preserve existing saved rules.

- Use frontend/icon.svg as the single source for application icons on all platforms. macOS editor processes use accessory activation policy after Wails launch, so configuration windows do not add Dock icons; independent main project windows retain normal app activation.

- Keep wails.json info.productVersion as the only application version source. Windows builds share scripts/windows-resources; resources.ps1 is its native build wrapper. Keep app and mounted DMG volume icons embedded, and apply the DMG file icon as macOS metadata.
