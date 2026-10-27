package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gytest-go/internal/core"
)

type App struct {
	ctx               context.Context
	engine            *core.Engine
	native            *nativeWindows
	remote            *nativeRemote
	nativeWatchCancel context.CancelFunc
	workspaceRole     string
	launchProject     func(projectWindowConfig) error
}

func NewApp() *App { return &App{engine: core.NewEngine(), workspaceRole: "client"} }
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.remote != nil {
		a.watchNativeWindow(ctx)
	}
}
func (a *App) domReady(ctx context.Context) {
	if a.remote == nil {
		runtime.WindowCenter(ctx)
	} else if c, err := a.GetNativeContext(); err == nil && c.Position != nil {
		runtime.WindowSetPosition(ctx, c.Position.X, c.Position.Y)
	}
}
func (a *App) shutdown(context.Context) {
	if a.remote != nil {
		if a.nativeWatchCancel != nil {
			a.nativeWatchCancel()
		}
		return
	}
	if a.native != nil {
		a.native.close()
	}
	a.engine.StopAll()
}
func (a *App) beforeClose(context.Context) bool {
	return false
}
func (a *App) GetSnapshot() core.Snapshot           { return a.engine.Snapshot() }
func (a *App) GetView(r core.ViewRequest) core.View { return a.engine.View(r) }
func (a *App) GetAutoData(channel, device string) (core.AutoDataSnapshot, error) {
	return a.engine.GetAutoData(channel, device)
}
func (a *App) SaveAutoData(channel, device string, c core.AutoDataConfig) (core.AutoDataSnapshot, error) {
	return a.engine.ConfigureAutoData(channel, device, "", c)
}
func (a *App) StartAutoData(channel, device string) (core.AutoDataSnapshot, error) {
	return a.engine.StartAutoData(channel, device)
}
func (a *App) SetAutoDataState(channel, device, action string) (core.AutoDataSnapshot, error) {
	return a.engine.SetAutoDataState(channel, device, action)
}
func (a *App) GetDataTest(channel, device, category string) (core.AutoDataSnapshot, error) {
	return a.engine.GetDataTest(channel, device, category)
}
func (a *App) SaveDataTest(channel, device, category string, config core.AutoDataConfig) (core.AutoDataSnapshot, error) {
	return a.engine.ConfigureAutoData(channel, device, category, config)
}
func (a *App) StartDataTest(channel, device, category string) (core.AutoDataSnapshot, error) {
	return a.engine.StartDataTest(channel, device, category)
}
func (a *App) SetDataTestState(channel, device, category, action string) (core.AutoDataSnapshot, error) {
	return a.engine.SetDataTestState(channel, device, category, action)
}
func (a *App) SetTelemetryRange(channel, device string, r core.TelemetryRangeEdit) (core.TelemetryRangeResult, error) {
	return a.engine.SetTelemetryRange(channel, device, r)
}
func (a *App) GetControlSimulation(channel, device, category string) (core.ControlSimulation, error) {
	return a.engine.GetControlSimulation(channel, device, category)
}
func (a *App) SaveControlSimulation(channel, device, category string, cfg core.ControlSimulation) error {
	return a.engine.ConfigureControlSimulation(channel, device, category, cfg)
}
func (a *App) PrepareAutoControl(channel, device, mode string, seed core.AutoControlSeed) (core.AutoControlSnapshot, error) {
	return a.engine.PrepareAutoControlDraft(channel, device, mode, seed)
}
func (a *App) GetAutoControlStatus(channel, device, mode string) (core.AutoControlStatus, error) {
	return a.engine.AutoControlStatus(channel, device, mode)
}
func (a *App) GetAutoControlDetails(channel, device, mode string, offset int) (core.AutoControlDetails, error) {
	return a.engine.AutoControlDetails(channel, device, mode, offset)
}
func (a *App) GetAutoControl(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.GetAutoControl(channel, device, mode)
}
func (a *App) GetAutoControlDraft(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.GetAutoControlDraft(channel, device, mode)
}
func (a *App) SaveAutoControlDraft(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.SaveAutoControlDraft(channel, device, mode)
}
func (a *App) SetAutoValueType(channel, device string, typ byte) error {
	return a.engine.SetAutoDraftValueType(channel, device, typ)
}
func (a *App) OpenAutoBulk(channel, device string) error {
	if err := a.engine.ValidateAutoControlWindow(channel, device, "yk"); err != nil {
		return err
	}
	return a.OpenNativeWindow(NativeContext{Kind: "auto-bulk-dialog", Channel: channel, Device: device, Role: "client", Point: "yk", Edit: true})
}
func (a *App) UpdateAutoOptions(channel, device, mode string, o core.AutoControlOptions) (core.AutoControlSnapshot, error) {
	if _, err := a.engine.UpdateAutoDraftOptions(channel, device, mode, o); err != nil {
		return core.AutoControlSnapshot{}, err
	}
	return a.engine.GetAutoControlDraft(channel, device, mode)
}
func (a *App) UpdateAutoRows(channel, device, mode string, rows []core.AutoControlRowEdit) (core.AutoControlSnapshot, error) {
	if _, err := a.engine.UpdateAutoDraftRows(channel, device, mode, rows); err != nil {
		return core.AutoControlSnapshot{}, err
	}
	return a.engine.GetAutoControlDraft(channel, device, mode)
}
func (a *App) ApplyAutoBulk(channel, device string, b core.AutoControlBulk) (core.AutoControlSnapshot, error) {
	if _, err := a.engine.ApplyAutoDraftBulk(channel, device, b); err != nil {
		return core.AutoControlSnapshot{}, err
	}
	return a.engine.GetAutoControlDraft(channel, device, "yk")
}
func (a *App) StartAutoControl(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.StartSavedAutoControl(channel, device, mode)
}
func (a *App) PauseAutoControl(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.PauseAutoControl(channel, device, mode)
}
func (a *App) ResumeAutoControl(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.ResumeAutoControl(channel, device, mode)
}
func (a *App) StopAutoControl(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.StopAutoControl(channel, device, mode)
}
func (a *App) SetWorkspaceMode(mode string) error {
	if mode != "client" && mode != "server" {
		return errors.New("无效工作模式")
	}
	if err := a.engine.EnsureWorkspaceChannel(mode); err != nil {
		return err
	}
	a.workspaceRole = mode
	a.updateMainWindowTitle(mode)
	return nil
}
func (a *App) UpsertChannel(c core.Channel) (string, error) { return a.engine.UpsertChannel(c) }
func (a *App) UpdateTransport(c core.Channel) error         { return a.engine.UpdateTransport(c) }
func (a *App) DeleteChannel(id string) error                { return a.engine.DeleteChannel(id) }
func (a *App) UpsertDevice(channelID, protocol string, d core.Device, replace bool) (string, error) {
	return a.engine.UpsertDevice(channelID, protocol, d, replace)
}
func (a *App) DeleteDevice(channelID, deviceID string) error {
	return a.engine.ConfigureDeleteDevice(channelID, deviceID)
}
func (a *App) RenameDevice(channelID, deviceID, name string) error {
	return a.engine.RenameDevice(channelID, deviceID, name)
}
func (a *App) PatchDevicePoint(channelID, deviceID string, key *core.PointKey, p core.Point, fields []string) error {
	return a.engine.ConfigureDevicePoint(channelID, deviceID, key, p, fields)
}
func (a *App) DeleteDevicePoint(channelID, deviceID string, key core.PointKey) error {
	return a.engine.ConfigureDeleteDevicePoint(channelID, deviceID, key)
}
func (a *App) ReadDeviceModbusPoints(channel, device string, fc byte, selection string) (core.PointOperationResult, error) {
	return a.engine.ReadDeviceModbusPoints(channel, device, fc, selection)
}
func (a *App) WriteDeviceModbusPoints(channel, device string, fc byte, selection string, value uint16) (core.PointOperationResult, error) {
	return a.engine.WriteDeviceModbusPoints(channel, device, fc, selection, value)
}
func (a *App) InterrogateDevice(channelID, deviceID string) error {
	return a.engine.InterrogateDevice(channelID, deviceID)
}
func (a *App) PublishDeviceCategory(channelID, deviceID, category string) error {
	return a.engine.PublishDeviceCategory(channelID, deviceID, category)
}
func (a *App) EditPointCell(channelID, deviceID string, key core.PointKey, field string, point core.Point) (core.PointValueResult, error) {
	return a.engine.ConfigurePointCell(channelID, deviceID, key, field, point)
}
func (a *App) SetPointValue(channelID, deviceID string, key core.PointKey, value float64) (core.PointValueResult, error) {
	return a.engine.SetPointValue(channelID, deviceID, key, value)
}
func (a *App) Command104Device(channelID, deviceID string, r core.Control104Request) (core.Control104Result, error) {
	return a.engine.Command104Device(channelID, deviceID, r)
}
func (a *App) StartChannel(id string) error            { return a.engine.Start(id) }
func (a *App) StopChannel(id string)                   { a.engine.Stop(id) }
func (a *App) StopAll()                                { a.engine.StopAll() }
func (a *App) ClearMessages()                          { a.engine.ClearMessages() }
func (a *App) ClearChannelMessages(id string)          { a.engine.ClearChannelMessages(id) }
func (a *App) RenameChannel(id, name string) error     { return a.engine.RenameChannel(id, name) }
func (a *App) SendHex(id, text string, crc bool) error { return a.engine.SendHex(id, text, crc) }
func (a *App) Interrogate(id string) error             { return a.engine.Interrogate(id) }
func (a *App) SerialPorts() ([]string, error)          { return core.SerialPorts() }
func (a *App) LocalIPs() ([]string, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	result := []string{"Any"}
	seen := map[string]bool{"Any": true}
	for _, address := range addresses {
		ip, _, e := net.ParseCIDR(address.String())
		if e == nil && ip.To4() != nil && !seen[ip.String()] {
			seen[ip.String()] = true
			result = append(result, ip.String())
		}
	}
	return result, nil
}
func (a *App) OpenProject() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "打开 protocol-simulator 工程", Filters: []runtime.FileFilter{{DisplayName: "protocol-simulator 工程", Pattern: "*.json"}}})
	if err != nil || path == "" {
		return "", err
	}
	if err = a.engine.LoadReplacing(path); err != nil {
		return "", err
	}
	a.updateMainWindowTitle("")
	return path, nil
}
func projectSaveFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "工程1"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	return name
}
func (a *App) SaveProject(saveAs bool) (string, error) {
	snapshot := a.engine.Snapshot()
	path := snapshot.Path
	if path == "" || saveAs {
		var err error
		path, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "保存 protocol-simulator 工程", DefaultFilename: projectSaveFilename(snapshot.Project.Name), Filters: []runtime.FileFilter{{DisplayName: "protocol-simulator 工程", Pattern: "*.json"}}})
		if err != nil || path == "" {
			return "", err
		}
	}
	if err := a.engine.Save(path); err != nil {
		return path, err
	}
	a.updateMainWindowTitle("")
	return path, nil
}
func (a *App) ExportMessageRows(rows []core.Message) (string, error) {
	if len(rows) > 2000 {
		return "", errors.New("报文超过缓存上限")
	}
	return a.exportMessageRows(rows)
}
func (a *App) exportMessageRows(rows []core.Message) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "导出报文", DefaultFilename: "报文.csv", Filters: []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}}})
	if err != nil || path == "" {
		return "", err
	}
	var b bytes.Buffer
	b.WriteString("\ufeff")
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"序号", "时间", "通道", "对端", "方向", "十六进制", "摘要"})
	for _, m := range rows {
		_ = w.Write([]string{strconv.FormatUint(m.Sequence, 10), m.Time, m.ChannelID, m.Peer, m.Direction, m.Hex, m.Summary})
	}
	w.Flush()
	if w.Error() != nil {
		return "", w.Error()
	}
	return path, core.AtomicWrite(path, b.Bytes())
}
func (a *App) ClearEvents(channel, category string) { a.engine.ClearEvents(channel, category) }
func (a *App) ClockSyncDevice(channel, device string) error {
	return a.engine.ClockSyncDevice(channel, device)
}
func (a *App) CounterInterrogateDevice(channel, device string) error {
	return a.engine.CounterInterrogateDevice(channel, device)
}

func (a *App) ReadClockDevice(channel, device string) error {
	return a.engine.ReadClockDevice(channel, device)
}
