package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	viewClock      uint64
	viewMetadata   uint64
	viewPoints     map[string]*pointRevision
	messageEpoch   uint64
	eventEpoch     uint64
	events         []PointEvent
	eventSequence  uint64
	dataGeneration uint64
	dataJobs       map[string]*autoDataJob
	autoMu         sync.Mutex
	autoJobs       map[string]*autoControlJob
	autoDrafts     map[string]*autoControlJob
	op             sync.Mutex
	mu             sync.Mutex
	project        Project
	running        map[string]*session
	states         map[string]ChannelState
	messages       []Message
	sequence       uint64
	path           string
	dirty          bool
}

func NewEngine() *Engine {
	return &Engine{viewClock: 1, viewMetadata: 1, dataGeneration: 1, project: Project{Version: 1, Name: "工程1", Channels: []Channel{}}, running: map[string]*session{}, states: map[string]ChannelState{}, messages: []Message{}}
}
func newID() string { b := make([]byte, 8); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func cloneProject(p Project) Project {
	p.Channels = append([]Channel{}, p.Channels...)
	for i := range p.Channels {
		p.Channels[i].TransportDraft = cloneTransportDraft(p.Channels[i].TransportDraft)
		p.Channels[i].Points = append([]Point{}, p.Channels[i].Points...)
		if p.Channels[i].Devices != nil {
			p.Channels[i].Devices = append([]Device{}, p.Channels[i].Devices...)
			for j := range p.Channels[i].Devices {
				p.Channels[i].Devices[j].Points = append([]Point{}, p.Channels[i].Devices[j].Points...)
				p.Channels[i].Devices[j].Configuration = cloneConfiguration(p.Channels[i].Devices[j].Configuration)
				p.Channels[i].Devices[j].AutoData = cloneAutoData(p.Channels[i].Devices[j].AutoData)
				p.Channels[i].Devices[j].DataTests = cloneDataTests(p.Channels[i].Devices[j].DataTests)
			}
			p.Channels[i].syncLegacy()
		}
	}
	return p
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	states := make([]ChannelState, 0, len(e.project.Channels))
	transfers := map[string]*session{}
	for _, c := range e.project.Channels {
		s, ok := e.states[c.ID]
		if !ok {
			s = ChannelState{ID: c.ID, Status: "stopped", Address: c.Address}
		}
		states = append(states, s)
		if c.Protocol == "iec104" {
			transfers[c.ID] = e.running[c.ID]
		}
	}
	snapshot := Snapshot{Project: cloneProject(e.project), States: states, Messages: append([]Message{}, e.messages...), Path: e.path, Dirty: e.dirty, Events: append([]PointEvent{}, e.events...)}
	e.mu.Unlock()
	// Peer locks must be taken after releasing Engine.mu: protocol handling logs
	// frames while holding its peer lock. TCP establishment alone is not STARTDT.
	for i := range snapshot.States {
		state := &snapshot.States[i]
		if transport, iec104 := transfers[state.ID]; iec104 {
			if transport != nil {
				state.ReadyPeers = len(readyDataPeers(transport))
			}
		} else {
			state.ReadyPeers = state.Peers
		}
	}
	return snapshot
}
func (e *Engine) log(id, peer, direction string, data []byte, summary string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.logLocked(id, peer, direction, data, summary)
}
func (e *Engine) logLocked(id, peer, direction string, data []byte, summary string) {
	e.sequence++
	e.messages = append(e.messages, Message{Sequence: e.sequence, Time: time.Now().Format("15:04:05.000"), ChannelID: id, Peer: peer, Direction: direction, Hex: strings.ToUpper(hex.EncodeToString(data)), Summary: summary})
	if len(e.messages) > 2000 {
		copy(e.messages, e.messages[len(e.messages)-1500:])
		e.messages = e.messages[:1500]
	}
	state := e.states[id]
	if direction == "TX" {
		state.TX += uint64(len(data))
	}
	if direction == "RX" {
		state.RX += uint64(len(data))
	}
	e.states[id] = state
}
func (e *Engine) state(s *session, fn func(*ChannelState)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[s.cfg.ID] != s {
		return
	}
	v := e.states[s.cfg.ID]
	fn(&v)
	e.states[s.cfg.ID] = v
}
func (e *Engine) ClearMessages() {
	e.mu.Lock()
	e.messages = []Message{}
	e.messageEpoch++
	e.mu.Unlock()
}
func (e *Engine) points(id string) []Point {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.ID == id {
			return append([]Point{}, c.Points...)
		}
	}
	return nil
}
func (e *Engine) applyPoints(id string, updates []Point) { e.applyDevicePoints(id, "", updates) }

func (e *Engine) UpsertChannel(c Channel) (string, error) {
	e.op.Lock()
	defer e.op.Unlock()
	c.Defaults()
	if err := c.Validate(); err != nil {
		return "", err
	}
	if c.ID == "" {
		c.ID = newID()
	}
	c = cloneProject(Project{Channels: []Channel{c}}).Channels[0]
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[c.ID] != nil {
		return "", errors.New("请先停止通道，再修改配置")
	}
	for i := range e.project.Channels {
		if e.project.Channels[i].ID == c.ID {
			e.project.Channels[i] = c
			e.dirty = true
			e.markConfigurationLocked()
			delete(e.states, c.ID)
			return c.ID, nil
		}
	}
	if len(e.project.Channels) >= 64 {
		return "", errors.New("最多支持 64 个通道")
	}
	e.project.Channels = append(e.project.Channels, c)
	e.dirty = true
	e.markConfigurationLocked()
	return c.ID, nil
}
func (e *Engine) DeleteChannel(id string) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[id] != nil {
		return errors.New("请先停止通道")
	}
	for i, c := range e.project.Channels {
		if c.ID == id {
			e.project.Channels = append(e.project.Channels[:i], e.project.Channels[i+1:]...)
			delete(e.states, id)
			e.dirty = true
			e.markConfigurationLocked()
			return nil
		}
	}
	return errors.New("通道不存在")
}
func (e *Engine) SetPoints(id string, points []Point) error { return e.SetDevicePoints(id, "", points) }
func (e *Engine) ReplaceProject(p Project, path string) error {
	e.op.Lock()
	defer e.op.Unlock()
	if err := validateProject(&p); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.running) > 0 {
		return errors.New("请先停止所有通道，再更换工程")
	}
	e.resetAutoControls()
	e.cancelDataLocked("", "")
	e.dataJobs = nil
	e.dataGeneration++
	e.project = cloneProject(p)
	e.markConfigurationLocked()
	e.messageEpoch++
	e.eventEpoch++
	e.path = path
	e.dirty = false
	e.states = map[string]ChannelState{}
	e.messages = []Message{}
	e.events = []PointEvent{}
	return nil
}

// Large projects are bounded on both read and write. Stream decoding avoids
// retaining a second raw byte buffer and string beside the decoded point tables.
const maxProjectFileBytes int64 = 512 << 20

func (e *Engine) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxProjectFileBytes {
		return fmt.Errorf("工程文件超过 %dMB", maxProjectFileBytes>>20)
	}
	reader := &io.LimitedReader{R: f, N: maxProjectFileBytes + 1}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var p Project
	if err = decoder.Decode(&p); err != nil {
		if reader.N == 0 {
			return fmt.Errorf("工程文件超过 %dMB", maxProjectFileBytes>>20)
		}
		return fmt.Errorf("工程解析失败：%w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if reader.N == 0 {
		return fmt.Errorf("工程文件超过 %dMB", maxProjectFileBytes>>20)
	}
	if err != io.EOF {
		return errors.New("工程文件包含多余内容")
	}
	return e.ReplaceProject(p, path)
}
func (e *Engine) Save(path string) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	filename := filepath.Base(path)
	name := filename
	if ext := filepath.Ext(filename); strings.EqualFold(ext, ".json") {
		name = strings.TrimSuffix(filename, ext)
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("请填写文件名，不能仅为 .json")
	}
	persisted := cloneProject(e.project)
	persisted.Name = name
	persisted.Version = 2
	if err := validateProject(&persisted); err != nil {
		return fmt.Errorf("工程未保存：%w", err)
	}
	for i := range persisted.Channels {
		persisted.Channels[i].Points = nil
	}
	b, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	if int64(len(b)) > maxProjectFileBytes {
		return fmt.Errorf("工程超过 %dMB，请减少点表后保存", maxProjectFileBytes>>20)
	}
	if err = AtomicWrite(path, b); err != nil {
		return err
	}
	e.project.Name = name
	e.path = path
	e.dirty = false
	return nil
}
func AtomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".gylab-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func (e *Engine) Start(id string) error {
	e.op.Lock()
	defer e.op.Unlock()
	return e.startLocked(id)
}
func (e *Engine) startLocked(id string) error {
	e.mu.Lock()
	existing := e.running[id]
	status := e.states[id].Status
	e.mu.Unlock()
	if existing != nil {
		if status != "disconnected" && status != "error" {
			return errors.New("通道已经启动")
		}
		e.stopLocked(id)
	}
	e.mu.Lock()
	var cfg Channel
	found := false
	for _, c := range e.project.Channels {
		if c.ID == id {
			cfg = c
			found = true
			break
		}
	}
	if !found {
		e.mu.Unlock()
		return errors.New("通道不存在")
	}
	cfg.Points = nil
	cfg.Devices = nil
	s := newSession(e, cfg)
	e.running[id] = s
	e.states[id] = ChannelState{ID: id, Status: "connecting", Address: cfg.Address}
	e.mu.Unlock()
	if err := s.open(); err != nil {
		s.close()
		e.mu.Lock()
		delete(e.running, id)
		e.states[id] = ChannelState{ID: id, Status: "error", Address: cfg.Address, LastError: err.Error()}
		e.mu.Unlock()
		return err
	}
	return nil
}
func (e *Engine) Stop(id string) { e.op.Lock(); defer e.op.Unlock(); e.stopLocked(id) }
func (e *Engine) stopLocked(id string) {
	e.stopAutoControls(id)
	e.mu.Lock()
	e.cancelDataLocked(id, "")
	s := e.running[id]
	delete(e.running, id)
	v := e.states[id]
	v.Status = "stopped"
	v.Peers = 0
	e.states[id] = v
	e.mu.Unlock()
	if s != nil {
		s.close()
		e.log(id, "", "SYS", nil, "通道已停止")
	}
}
func (e *Engine) StopAll() {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	ids := make([]string, 0, len(e.running))
	for id := range e.running {
		ids = append(ids, id)
	}
	e.mu.Unlock()
	for _, id := range ids {
		e.stopLocked(id)
	}
}
func (e *Engine) getSession(id string) (*session, error) {
	e.mu.Lock()
	s := e.running[id]
	e.mu.Unlock()
	if s == nil {
		return nil, errors.New("请先启动通道")
	}
	return s, nil
}
func (e *Engine) SendHex(id, text string, crc bool) error {
	s, err := e.getSession(id)
	if err != nil {
		return err
	}
	clean := strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "", ":", "").Replace(strings.TrimSpace(text))
	if len(clean) == 0 || len(clean) > 8192 {
		return errors.New("请输入 1–4096 字节的十六进制报文")
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return errors.New("十六进制内容无效，应为完整的字节对")
	}
	if crc {
		n := CRC16(b)
		b = append(b, byte(n), byte(n>>8))
	}
	if s.cfg.Protocol != "raw" {
		return errors.New("原始报文发送仅在 RAW 通道启用，避免破坏协议序号")
	}
	return s.broadcastRaw(b)
}
func (e *Engine) ReadModbus(id string, fc byte, start, count uint16) error {
	return e.ReadDeviceModbus(id, "", fc, start, count)
}
func (e *Engine) WriteModbus(id string, fc byte, address, value uint16) error {
	return e.WriteDeviceModbus(id, "", fc, address, value)
}
func (e *Engine) Interrogate(id string) error { return e.InterrogateDevice(id, "") }
func (e *Engine) Publish(id string) error     { return e.PublishDevice(id, "") }

func (e *Engine) Demo(protocol string) (string, error) {
	if protocol != "iec104" && protocol != "modbus-tcp" {
		return "", errors.New("不支持的演示协议")
	}
	points := []Point{{Address: 0, Name: "开关状态", Kind: "coil", Value: 1}, {Address: 0, Name: "测试电压", Kind: "holding", Value: 220}, {Address: 1, Name: "测试电流", Kind: "holding", Value: 12}}
	if protocol == "iec104" {
		points = []Point{{Address: 1, Name: "开关状态", Kind: "yx", Value: 1}, {Address: 16385, Name: "测试电压", Kind: "yc", Value: 220.5}, {Address: 16386, Name: "测试电流", Kind: "yc", Value: 12.3}}
	}
	server := Channel{ID: newID(), Name: "本机子站", Protocol: protocol, Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, CommonAddress: 1, Points: points}
	server.Defaults()
	if err := e.ReplaceProjectAndStop(Project{Version: 1, Name: "本地联调 · " + protocol, Channels: []Channel{server}}, ""); err != nil {
		return "", err
	}
	if err := e.Start(server.ID); err != nil {
		return "", err
	}
	snap := e.Snapshot()
	address := snap.States[0].Address
	client := server
	client.ID = newID()
	client.Name = "本机主站"
	client.Role = "client"
	client.Transport = "tcp-client"
	client.Address = address
	client.Points = []Point{}
	client.Devices = nil
	id, err := e.UpsertChannel(client)
	if err != nil {
		e.StopAll()
		return "", err
	}
	if err = e.Start(id); err != nil {
		e.StopAll()
		return "", err
	}
	return id, nil
}

func (e *Engine) DeviceWindowGeneration() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dataGeneration
}
