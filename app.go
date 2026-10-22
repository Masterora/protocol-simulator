package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net"
	"os"
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
	if a.remote != nil {
		if reusableNativeWindow(a.remote.kind) && !a.remote.exiting.Load() {
			runtime.WindowHide(a.ctx)
			return true
		}
		return false
	}
	return false
}
func (a *App) GetSnapshot() core.Snapshot { return a.engine.Snapshot() }
func (a *App) GetAutoData(channel, device string) (core.AutoDataSnapshot, error) {
	return a.engine.GetAutoData(channel, device)
}
func (a *App) SaveAutoData(channel, device string, c core.AutoDataConfig) (core.AutoDataSnapshot, error) {
	return a.engine.SaveAutoData(channel, device, c)
}
func (a *App) StartAutoData(channel, device string) (core.AutoDataSnapshot, error) {
	return a.engine.StartAutoData(channel, device)
}
func (a *App) SetAutoDataState(channel, device, action string) (core.AutoDataSnapshot, error) {
	return a.engine.SetAutoDataState(channel, device, action)
}
func (a *App) PrepareAutoControl(channel, device, mode string, seed core.AutoControlSeed) (core.AutoControlSnapshot, error) {
	return a.engine.PrepareAutoControl(channel, device, mode, seed)
}
func (a *App) GetAutoControl(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.GetAutoControl(channel, device, mode)
}
func (a *App) SetAutoValueType(channel, device string, typ byte) error {
	return a.engine.SetAutoValueType(channel, device, typ)
}
func (a *App) OpenAutoBulk(channel, device string) error {
	if err := a.engine.ValidateAutoControlWindow(channel, device, "yk"); err != nil {
		return err
	}
	return a.OpenNativeWindow(NativeContext{Kind: "auto-bulk-dialog", Channel: channel, Device: device, Role: "client", Point: "yk", Edit: true})
}
func (a *App) UpdateAutoOptions(channel, device, mode string, o core.AutoControlOptions) (core.AutoControlSnapshot, error) {
	return a.engine.UpdateAutoOptions(channel, device, mode, o)
}
func (a *App) UpdateAutoRows(channel, device, mode string, rows []core.AutoControlRowEdit) (core.AutoControlSnapshot, error) {
	return a.engine.UpdateAutoRows(channel, device, mode, rows)
}
func (a *App) ApplyAutoBulk(channel, device string, b core.AutoControlBulk) (core.AutoControlSnapshot, error) {
	return a.engine.ApplyAutoBulk(channel, device, b)
}
func (a *App) StartAutoControl(channel, device, mode string) (core.AutoControlSnapshot, error) {
	return a.engine.StartAutoControl(channel, device, mode)
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
func (a *App) SetPoints(id string, points []core.Point) error {
	return a.engine.SetPoints(id, points)
}
func (a *App) PatchPoint(id string, key *core.PointKey, point core.Point, fields []string) error {
	return a.engine.PatchPoint(id, key, point, fields)
}
func (a *App) DeletePoint(id string, key core.PointKey) error { return a.engine.DeletePoint(id, key) }
func (a *App) UpsertDevice(channelID, protocol string, d core.Device, replace bool) (string, error) {
	return a.engine.UpsertDevice(channelID, protocol, d, replace)
}
func (a *App) DeleteDevice(channelID, deviceID string) error {
	return a.engine.DeleteDevice(channelID, deviceID)
}
func (a *App) RenameDevice(channelID, deviceID, name string) error {
	return a.engine.RenameDevice(channelID, deviceID, name)
}
func (a *App) PatchDevicePoint(channelID, deviceID string, key *core.PointKey, p core.Point, fields []string) error {
	return a.engine.PatchDevicePoint(channelID, deviceID, key, p, fields)
}
func (a *App) DeleteDevicePoint(channelID, deviceID string, key core.PointKey) error {
	return a.engine.DeleteDevicePoint(channelID, deviceID, key)
}
func (a *App) ReadDeviceModbus(channelID, deviceID string, fc byte, start, count uint16) error {
	return a.engine.ReadDeviceModbus(channelID, deviceID, fc, start, count)
}
func (a *App) WriteDeviceModbus(channelID, deviceID string, fc byte, address, value uint16) error {
	return a.engine.WriteDeviceModbus(channelID, deviceID, fc, address, value)
}
func (a *App) InterrogateDevice(channelID, deviceID string) error {
	return a.engine.InterrogateDevice(channelID, deviceID)
}
func (a *App) PublishDevice(channelID, deviceID string) error {
	return a.engine.PublishDevice(channelID, deviceID)
}
func (a *App) EditPointCell(channelID, deviceID string, key core.PointKey, field string, point core.Point) (core.PointValueResult, error) {
	return a.engine.EditPointCell(channelID, deviceID, key, field, point)
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
func (a *App) ReadModbus(id string, fc byte, start, count uint16) error {
	return a.engine.ReadModbus(id, fc, start, count)
}
func (a *App) WriteModbus(id string, fc byte, address, value uint16) error {
	return a.engine.WriteModbus(id, fc, address, value)
}
func (a *App) Interrogate(id string) error    { return a.engine.Interrogate(id) }
func (a *App) Publish(id string) error        { return a.engine.Publish(id) }
func (a *App) SerialPorts() ([]string, error) { return core.SerialPorts() }
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
func (a *App) StartDemo(protocol string) (string, error) { return a.engine.Demo(protocol) }
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
func (a *App) ExportMessages() (string, error) {
	return a.exportMessageRows(a.engine.Snapshot().Messages)
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
func (a *App) ExportPoints(id string) (string, error) { return a.ExportDevicePoints(id, "") }
func (a *App) ExportDevicePoints(id, deviceID string) (string, error) {
	d, err := a.engine.Device(id, deviceID)
	if err != nil {
		return "", err
	}
	points := d.Points
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "导出点表", DefaultFilename: "点表.csv", Filters: []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}}})
	if err != nil || path == "" {
		return "", err
	}
	data, err := encodePointCSV(points)
	if err != nil {
		return "", err
	}
	return path, core.AtomicWrite(path, data)
}
func (a *App) ImportPoints(id string) (int, error) { return a.ImportDevicePoints(id, "") }
func (a *App) ImportDevicePoints(id, deviceID string) (int, error) {
	if _, err := a.engine.Device(id, deviceID); err != nil {
		return 0, err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "导入点表", Filters: []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}}})
	if err != nil || path == "" {
		return 0, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if stat.Size() > 4*1024*1024 {
		return 0, errors.New("点表文件超过 4MB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	points, err := decodePointCSV(data)
	if err != nil {
		return 0, err
	}
	return len(points), a.engine.SetDevicePoints(id, deviceID, points)
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
