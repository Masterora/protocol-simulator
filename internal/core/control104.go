package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// Type 48 uses the signed 16-bit normalized wire value, as in GYTest's
// "归一化" editor. This is distinct from a floating point value in [-1,1).
type Control104Request struct {
	Timestamp string  `json:"timestamp,omitempty"`
	Type      byte    `json:"type"`
	Address   uint32  `json:"address"`
	Value     float64 `json:"value"`
	Operation string  `json:"operation"`
	Direct    bool    `json:"direct"`
	TimeoutMS int     `json:"timeoutMs"`
	batchID   string
}
type Control104Result struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}
type controlOutcome struct {
	result Control104Result
	err    error
}
type controlWait struct {
	asdu      []byte
	request   Control104Request
	result    chan controlOutcome
	activated bool
}
type controlKey struct {
	ca      uint16
	kind    string
	address uint32
}
type controlSelection struct {
	signature []byte
	until     time.Time
}

// STOPDT invalidates selections. A command interrupted mid-flight remains
// uncertain until reconnect, so its late reply cannot satisfy a later command.
func (p *peer) abortControlLocked() {
	p.delayed = nil
	p.scheduled = nil
	p.nextRun = nil
	p.clientSelections = nil
	p.serverSelections = nil
	if p.control != nil {
		p.controlUncertain = true
		select {
		case p.control.result <- controlOutcome{err: errors.New("数据传输已停止，命令结果未知")}:
		default:
		}
		p.control = nil
	}
}

func controlKind(typ byte) string {
	if typ >= 58 && typ <= 63 {
		typ -= 13
	}

	switch typ {
	case 45, 46:
		return "yk"
	case 47:
		return "step"
	case 48, 49, 50:
		return "yt"
	}
	return ""
}
func controlName(typ byte) string {
	if typ >= 58 && typ <= 63 {
		typ -= 13
	}

	switch typ {
	case 45:
		return "单点遥控"
	case 46:
		return "双点遥控"
	case 47:
		return "遥调（步调节）"
	case 48:
		return "遥调（归一化原码）"
	case 49:
		return "遥调（标度化）"
	case 50:
		return "遥调（浮点设值）"
	}
	return "控制"
}
func controlWidth(typ byte) int {
	if typ >= 58 && typ <= 63 {
		typ -= 13
	}

	switch typ {
	case 45, 46, 47:
		return 1
	case 48, 49:
		return 3
	case 50:
		return 5
	}
	return 0
}

func EncodeControl104(ca uint16, r Control104Request) ([]byte, error) {
	if r.Type >= 58 && r.Type <= 63 {
		original := r.Type
		r.Type -= 13
		a, err := EncodeControl104(ca, r)
		if err != nil {
			return nil, err
		}
		t := time.Now()
		if r.Timestamp != "" {
			t, err = time.Parse(time.RFC3339Nano, r.Timestamp)
			if err != nil {
				return nil, err
			}
		}
		a[0] = original
		return append(a, encodeCP56(t)...), nil
	}

	if ca == 0 || ca == 65535 || r.Address > 0xffffff || controlKind(r.Type) == "" {
		return nil, errors.New("控制类型或点位无效")
	}
	if math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
		return nil, errors.New("控制值必须是有限数值")
	}
	cot := byte(6)
	switch r.Operation {
	case "select", "execute":
	case "cancel":
		cot = 8
	default:
		return nil, errors.New("操作应为选择、执行或撤销")
	}
	data := make([]byte, controlWidth(r.Type))
	switch r.Type {
	case 45:
		if r.Value != 0 && r.Value != 1 {
			return nil, errors.New("单点遥控值应为 0 或 1")
		}
		data[0] = byte(r.Value)
	case 46, 47:
		if r.Value != 1 && r.Value != 2 {
			return nil, errors.New("双点／步调节值应为 1 或 2")
		}
		data[0] = byte(r.Value)
	case 48, 49:
		if r.Value < -32768 || r.Value > 32767 || math.Trunc(r.Value) != r.Value {
			return nil, errors.New("归一化原码／标度值应为 -32768–32767 整数")
		}
		binary.LittleEndian.PutUint16(data, uint16(int16(r.Value)))
	case 50:
		if math.Abs(r.Value) > math.MaxFloat32 {
			return nil, errors.New("设值超出短浮点范围")
		}
		binary.LittleEndian.PutUint32(data, math.Float32bits(float32(r.Value)))
	}
	if r.Operation == "select" {
		data[len(data)-1] |= 0x80
	}
	return ASDU(r.Type, 1, cot, ca, append(ioa(r.Address), data...)), nil
}
func decodeControl104(a []byte) (Control104Request, error) {
	if len(a) > 0 && a[0] >= 58 && a[0] <= 63 {
		if len(a) != 16+controlWidth(a[0]) {
			return Control104Request{}, errors.New("带时标控制长度无效")
		}
		stamp, invalid, err := decodeCP56(a[len(a)-7:])
		if err != nil || invalid {
			return Control104Request{}, errors.New("控制时标无效")
		}
		base := append([]byte{}, a[:len(a)-7]...)
		base[0] -= 13
		r, err := decodeControl104(base)
		r.Type = a[0]
		r.Timestamp = stamp
		return r, err
	}

	if len(a) < 9 || controlWidth(a[0]) == 0 || a[1] != 1 || len(a) != 9+controlWidth(a[0]) {
		return Control104Request{}, errors.New("控制 ASDU 数量或长度无效")
	}
	r := Control104Request{Type: a[0], Address: uint32(a[6]) | uint32(a[7])<<8 | uint32(a[8])<<16, Operation: "execute"}
	v := a[9:]
	qualifier := v[len(v)-1]
	if qualifier&0x80 != 0 {
		r.Operation = "select"
	}
	if a[2]&0x3f == 8 || a[2]&0x3f == 9 {
		r.Operation = "cancel"
	}
	switch r.Type {
	case 45:
		if qualifier&2 != 0 {
			return r, errors.New("单点命令保留位无效")
		}
		r.Value = float64(v[0] & 1)
	case 46, 47:
		r.Value = float64(v[0] & 3)
	case 48, 49:
		r.Value = float64(int16(binary.LittleEndian.Uint16(v)))
	case 50:
		r.Value = float64(math.Float32frombits(binary.LittleEndian.Uint32(v)))
	}
	_, err := EncodeControl104(binary.LittleEndian.Uint16(a[4:]), r)
	return r, err
}
func controlSignature(a []byte) []byte {
	if a[0] >= 58 && a[0] <= 63 {
		a = a[:len(a)-7]
	}
	v := append([]byte{}, a...)
	v[2] = 0
	v[len(v)-1] &= 0x7f
	return v
}
func controlID(a []byte) controlKey {
	return controlKey{binary.LittleEndian.Uint16(a[4:]), controlKind(a[0]), uint32(a[6]) | uint32(a[7])<<8 | uint32(a[8])<<16}
}

func (e *Engine) Command104Device(channelID, deviceID string, r Control104Request) (Control104Result, error) {
	return e.command104DeviceContext(context.Background(), channelID, deviceID, r)
}
func (e *Engine) command104DeviceContext(ctx context.Context, channelID, deviceID string, r Control104Request) (Control104Result, error) {
	if err := ctx.Err(); err != nil {
		return Control104Result{}, err
	}
	s, err := e.getSession(channelID)
	if err != nil {
		return Control104Result{}, err
	}
	if s.cfg.Protocol != "iec104" || s.cfg.Role != "client" {
		return Control104Result{}, errors.New("控制操作仅适用于 IEC104 主站")
	}
	d, err := e.Device(channelID, deviceID)
	if err != nil {
		return Control104Result{}, err
	}
	a, err := EncodeControl104(d.CommonAddress, r)
	if err != nil {
		return Control104Result{}, err
	}
	if r.TimeoutMS == 0 {
		r.TimeoutMS = s.cfg.TimeoutMS
	}
	if r.TimeoutMS < 100 || r.TimeoutMS > 60000 {
		return Control104Result{}, errors.New("应答超时应为 100–60000 毫秒")
	}
	var result Control104Result
	err = s.forPeers(func(p *peer) error {
		var failure error
		result, failure = p.command104Context(ctx, a, r)
		return failure
	})
	return result, err
}
func (p *peer) command104(a []byte, r Control104Request) (Control104Result, error) {
	return p.command104Context(context.Background(), a, r)
}

var errControlUncertain = errors.New("上次命令结果未确定，请重连后操作")

func (p *peer) command104Context(ctx context.Context, a []byte, r Control104Request) (Control104Result, error) {
	p.mu.Lock()
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return Control104Result{}, err
	}
	if p.batchOwner != r.batchID && (p.batchOwner != "" || r.batchID != "") {
		p.mu.Unlock()
		return Control104Result{}, errors.New("通道正在执行自动任务，请先结束自动操作")
	}
	if p.closed || !p.transfer {
		p.mu.Unlock()
		return Control104Result{}, errors.New("104 数据传输尚未激活")
	}
	if p.control != nil {
		p.mu.Unlock()
		return Control104Result{}, errors.New("已有控制命令等待应答")
	}
	if p.controlUncertain {
		p.mu.Unlock()
		return Control104Result{}, errControlUncertain
	}
	for k, v := range p.clientSelections {
		if time.Now().After(v.until) {
			delete(p.clientSelections, k)
		}
	}
	key := controlID(a)
	selected, ok := p.clientSelections[key]
	if r.Operation == "execute" && !r.Direct && (!ok || time.Now().After(selected.until) || !bytes.Equal(selected.signature, controlSignature(a))) {
		p.mu.Unlock()
		return Control104Result{}, errors.New("请先选择相同的类型、点位和值，并等待选择确认")
	}
	delete(p.clientSelections, key)
	wait := &controlWait{asdu: a, request: r, result: make(chan controlOutcome, 1)}
	p.control = wait
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.control == wait {
			p.control = nil
		}
		p.mu.Unlock()
	}()
	if err := p.sendIContext(ctx, a, controlName(r.Type)+" · "+r.Operation); err != nil {
		return Control104Result{}, err
	}
	timer := time.NewTimer(time.Duration(r.TimeoutMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		p.mu.Lock()
		p.controlUncertain = true
		p.mu.Unlock()
		return Control104Result{}, errors.New("自动操作已结束，在途命令结果未确定，请重连后操作")
	case outcome := <-wait.result:
		return outcome.result, outcome.err
	case <-p.done:
		return Control104Result{}, errors.New("连接已断开，命令结果未知")
	case <-timer.C:
		p.mu.Lock()
		p.controlUncertain = true
		p.mu.Unlock()
		err := errors.New("命令应答超时，执行结果未知")
		p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, controlName(r.Type)+"："+err.Error())
		return Control104Result{}, err
	}
}
func (p *peer) controlReply104(a []byte) error {
	r, err := decodeControl104(a)
	if err != nil {
		p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, err.Error())
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.control
	if w == nil || !bytes.Equal(controlSignature(w.asdu), controlSignature(a)) || w.request.Operation == "select" && r.Operation != "select" || w.request.Operation != "select" && r.Operation == "select" {
		p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, "忽略未匹配的控制应答")
		return nil
	}
	cot := a[2] & 0x3f
	complete := func(stage, message string, err error) {
		p.control = nil
		select {
		case w.result <- controlOutcome{Control104Result{stage, message}, err}:
		default:
		}
		direction := "SYS"
		if err != nil {
			direction = "ERR"
		}
		p.s.e.log(p.s.cfg.ID, p.address, direction, nil, controlName(r.Type)+"："+message)
	}
	if a[2]&0x80 != 0 {
		complete("rejected", "收到测试应答，未确认实际执行", errors.New("收到测试应答"))
		return nil
	}
	if a[2]&0x40 != 0 || cot >= 44 && cot <= 47 {
		message := fmt.Sprintf("子站拒绝命令（原因 %d）", cot)
		complete("rejected", message, errors.New(message))
		return nil
	}
	switch {
	case w.request.Operation == "select" && cot == 7:
		if p.clientSelections == nil {
			p.clientSelections = map[controlKey]controlSelection{}
		}
		p.clientSelections[controlID(a)] = controlSelection{controlSignature(a), time.Now().Add(30 * time.Second)}
		complete("selected", "选择确认", nil)
	case w.request.Operation == "cancel" && cot == 9:
		complete("cancelled", "撤销确认", nil)
	case w.request.Operation == "execute" && cot == 7:
		w.activated = true
	case w.request.Operation == "execute" && cot == 10 && w.activated:
		complete("completed", "执行结束", nil)
	default:
		p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, "控制应答顺序或传送原因不匹配")
	}
	return nil
}

func (e *Engine) applyControl104(channelID, deviceID string, r Control104Request, apply bool) ([]Point, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channelID {
			continue
		}
		d, err := deviceIn(c, deviceID)
		if err != nil {
			return nil, err
		}
		for j := range d.Points {
			point := &d.Points[j]
			if (point.Kind != controlKind(r.Type) && !(controlKind(r.Type) == "step" && point.Kind == "yt")) || point.Address != r.Address {
				continue
			}
			if !apply {
				return nil, nil
			}
			value := r.Value
			if r.Type == 46 || r.Type == 59 {
				value = r.Value - 1
			}
			if r.Type == 47 || r.Type == 60 {
				value = point.Value - 1
				if r.Value == 2 {
					value = point.Value + 1
				}
			}
			updated := *point
			updated.Value = value
			updated.WireType = r.Type
			if err := validatePoints([]Point{updated}, "iec104"); err != nil {
				return nil, err
			}
			mapped, err := mappedControlPoints("iec104", *d, []Point{updated})
			if err != nil {
				return nil, err
			}
			for _, u := range mapped {
				for k := range d.Points {
					if d.Points[k].Kind == u.Kind && d.Points[k].Address == u.Address {
						d.Points[k] = u
						break
					}
				}
			}
			point.Value = value
			point.Updated = time.Now().Format("15:04:05.000")
			c.syncLegacy()
			e.dirty = true
			e.markPointsLocked(channelID, d.ID, append(mapped, *point))
			return mapped, nil
		}
		return nil, errors.New("未配置对应的控制点")
	}
	return nil, errors.New("装置已被移除")
}
func (p *peer) controlRequest104(d Device, a []byte) error {
	r, err := decodeControl104(a)
	reply := func(cot byte, message string) error {
		out := append([]byte{}, a...)
		out[2] = cot
		return p.sendI(out, message)
	}
	if err != nil {
		return reply(7|0x40, err.Error())
	}
	cot := a[2] & 0x3f
	if a[2]&0xc0 != 0 || cot != 6 && cot != 8 {
		return reply(45|0x40, "无效控制传送原因／测试标志")
	}
	if _, err = p.s.e.applyControl104(p.s.cfg.ID, d.ID, r, false); err != nil {
		return reply(47|0x40, err.Error())
	}
	key, signature := controlID(a), controlSignature(a)
	p.mu.Lock()
	if p.serverSelections == nil {
		p.serverSelections = map[controlKey]controlSelection{}
	}
	selected, exists := p.serverSelections[key]
	if r.Operation == "select" {
		p.serverSelections[key] = controlSelection{signature, time.Now().Add(30 * time.Second)}
		p.mu.Unlock()
		return reply(7, "选择确认")
	}
	if r.Operation == "cancel" {
		if !exists || !bytes.Equal(selected.signature, signature) {
			p.mu.Unlock()
			return reply(9|0x40, "无匹配的选择，撤销否认")
		}
		delete(p.serverSelections, key)
		p.mu.Unlock()
		return reply(9, "撤销确认")
	}
	if exists && (time.Now().After(selected.until) || !bytes.Equal(selected.signature, signature)) {
		p.mu.Unlock()
		return reply(7|0x40, "选择已超时或命令不匹配")
	}
	delete(p.serverSelections, key)
	p.mu.Unlock()
	mapped, err := p.s.e.applyControl104(p.s.cfg.ID, d.ID, r, true)
	if err != nil {
		return reply(7|0x40, err.Error())
	}
	if err = reply(7, "执行确认"); err != nil {
		return err
	}
	if err = reply(10, "执行结束"); err != nil {
		return err
	}
	for _, other := range readyDataPeers(p.s) {
		if err = other.publishPoints(d, mapped, 3); err != nil {
			return err
		}
	}
	return nil
}
