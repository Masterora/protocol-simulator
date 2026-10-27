package core

import (
	"fmt"
	"time"
)

type scheduledCommand struct {
	asdu    []byte
	at      time.Time
	retries int
}
type delayedASDU struct {
	asdu []byte
	at   time.Time
}

func (p *peer) device104() Device {
	// Framing, acknowledgements and timers only need link settings. Copying the
	// full point table here allocates megabytes for every frame at large scales.
	e := p.s.e
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.ID != p.s.cfg.ID || len(c.Devices) == 0 {
			continue
		}
		source := c.Devices[0]
		d := Device{ID: source.ID, Name: source.Name, Unit: source.Unit, CommonAddress: source.CommonAddress}
		if source.Configuration != nil {
			d.Configuration = &DeviceConfiguration{Version: source.Configuration.Version, Values: make(map[string]string, len(source.Configuration.Values))}
			for key, v := range source.Configuration.Values {
				d.Configuration.Values[key] = v
			}
		}
		return d
	}
	return Device{}
}
func (p *peer) tracked104(d Device, a []byte, now time.Time) error {
	p.mu.Lock()
	if p.scheduled == nil {
		p.scheduled = map[byte]scheduledCommand{}
	}
	if _, ok := p.scheduled[a[0]]; ok {
		p.mu.Unlock()
		return nil
	}
	p.scheduled[a[0]] = scheduledCommand{append([]byte{}, a...), now, 0}
	p.mu.Unlock()
	if err := p.sendI(a, "周期命令 · "+d.Name); err != nil {
		p.mu.Lock()
		delete(p.scheduled, a[0])
		p.mu.Unlock()
		return err
	}
	return nil
}
func (p *peer) periodic(now time.Time) {
	if p.s.cfg.Role != "client" {
		return
	}
	if p.s.cfg.Protocol == "iec104" {
		p.mu.Lock()
		ready := p.transfer && !p.closed
		p.mu.Unlock()
		if !ready {
			return
		}
		d := p.device104()
		if d.ID == "" {
			return
		}
		commands := []struct {
			key    string
			period time.Duration
			a      []byte
		}{
			{"total", seconds(d, "txtTotal_Time", 0), interrogation(d.CommonAddress, 6)},
			{"pulse", time.Duration(number(d, "txtMC_Time", 0)) * time.Minute, ASDU(101, 1, 6, d.CommonAddress, []byte{0, 0, 0, 5})},
			{"clock", time.Duration(number(d, "txtSyn_Time", 0)) * time.Minute, ASDU(103, 1, 6, d.CommonAddress, append([]byte{0, 0, 0}, encodeCP56(now)...))},
		}
		for _, c := range commands {
			if c.key == "pulse" && !flag(d, "mcEnable", false) || c.key == "clock" && !flag(d, "timeEnable", false) || c.period <= 0 {
				continue
			}
			if p.due(c.key, c.period, now) {
				if err := p.tracked104(d, c.a, now); err != nil {
					p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, err.Error())
				}
			}
		}
		p.mu.Lock()
		resend := [][]byte{}
		for typ, cmd := range p.scheduled {
			interval := time.Duration(p.s.cfg.TimeoutMS) * time.Millisecond
			if flag(d, "cbxRetry", false) {
				interval = seconds(d, "retryInterval", 2)
			}
			if now.Sub(cmd.at) < interval {
				continue
			}
			if flag(d, "cbxRetry", false) && cmd.retries < number(d, "retryNum", 3) {
				cmd.retries++
				cmd.at = now
				p.scheduled[typ] = cmd
				resend = append(resend, cmd.asdu)
			} else {
				delete(p.scheduled, typ)
				p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, fmt.Sprintf("104 命令 %d 应答超时", typ))
			}
		}
		p.mu.Unlock()
		for _, a := range resend {
			if err := p.sendI(a, "104 命令超时重发"); err != nil {
				p.fail(err)
				return
			}
		}
	} else if p.s.cfg.Protocol == "modbus-tcp" || p.s.cfg.Protocol == "modbus-rtu" {
		for _, entry := range p.s.e.devices(p.s.cfg.ID) {
			d, err := p.s.e.Device(p.s.cfg.ID, entry.ID)
			if err != nil || !flag(d, "pollEnabled", false) || d.Unit == 0 {
				continue
			}
			period := time.Duration(number(d, "txtSendInterval", 200)) * time.Millisecond
			p.mu.Lock()
			pending := len(p.pending) > 0 || p.batchOwner != "" || p.controlUncertain
			p.mu.Unlock()
			if pending {
				return
			}
			if flag(d, "cbTime", false) && number(d, "SynTime", 0) > 0 && p.due(d.ID+"/clock", seconds(d, "SynTime", 10), now) {
				_ = p.s.requestDeviceModbus(d, modbusClockPDU(d, now))
				return
			}
			if !p.due(d.ID+"/poll", period, now) {
				continue
			}
			tasks := modbusPollTasks(d)
			if len(tasks) == 0 {
				continue
			}
			p.mu.Lock()
			index := p.pollIndex[d.ID] % len(tasks)
			p.pollIndex[d.ID]++
			p.mu.Unlock()
			if err = p.s.requestDeviceModbus(d, tasks[index]); err != nil {
				p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, err.Error())
			}
			return
		}
	}
}
func (p *peer) due(key string, period time.Duration, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nextRun == nil {
		p.nextRun = map[string]time.Time{}
	}
	if p.pollIndex == nil {
		p.pollIndex = map[string]int{}
	}
	next, ok := p.nextRun[key]
	if !ok {
		p.nextRun[key] = now.Add(period)
		return false
	}
	if now.Before(next) {
		return false
	}
	p.nextRun[key] = now.Add(period)
	return true
}
func modbusPollTasks(d Device) [][]byte {
	tasks := [][]byte{}
	if d.Configuration == nil {
		return tasks
	}
	for _, r := range d.Configuration.Ranges {
		if r.Tab == "yk" || r.Tab == "yt" || r.Count == 0 {
			continue
		}
		fc := map[string]byte{"coil": 1, "discrete": 2, "holding": 3, "input": 4}[r.Kind]
		if fc == 0 {
			continue
		}
		width := 1
		for _, p := range d.Points {
			if p.Tab == r.Tab {
				width = registerWidth(p)
				break
			}
		}
		max := 125
		if fc <= 2 {
			max = 2000
		}
		max -= max % width
		points, err := rangePointNumbers("modbus-tcp", d.Configuration, r)
		if err != nil {
			continue
		}
		for i := 0; i < len(points); {
			start := points[i]
			count := width
			i++
			for i < len(points) && points[i] == start+uint32(count) && count+width <= max {
				count += width
				i++
			}
			if a, err := ReadPDU(fc, uint16(start), uint16(count)); err == nil {
				tasks = append(tasks, a)
			}
		}

	}
	if flag(d, "cb_ReadEvent", false) {
		addr, _ := parseAddress(value(d, "txtEventAddr", "500H"), 65535)
		if a, err := ReadPDU(3, uint16(addr), uint16(number(d, "eventCount", 16))); err == nil {
			tasks = append(tasks, a)
		}
	}
	return tasks
}
func (e *Engine) ClockSyncDevice(channel, device string) error {
	s, err := e.getSession(channel)
	if err != nil {
		return err
	}
	d, err := e.Device(channel, device)
	if err != nil {
		return err
	}
	if s.cfg.Role != "client" {
		return fmt.Errorf("校时需要主站")
	}
	if s.cfg.Protocol == "modbus-tcp" || s.cfg.Protocol == "modbus-rtu" {
		return s.requestDeviceModbus(d, modbusClockPDU(d, time.Now()))
	}
	if s.cfg.Protocol != "iec104" {
		return fmt.Errorf("规约不支持校时")
	}
	a := ASDU(103, 1, 6, d.CommonAddress, append([]byte{0, 0, 0}, encodeCP56(time.Now())...))
	return s.forPeers(func(p *peer) error { return p.tracked104(d, a, time.Now()) })
}
func (e *Engine) CounterInterrogateDevice(channel, device string) error {
	s, err := e.getSession(channel)
	if err != nil {
		return err
	}
	d, err := e.Device(channel, device)
	if err != nil {
		return err
	}
	if s.cfg.Protocol != "iec104" || s.cfg.Role != "client" {
		return fmt.Errorf("脉冲召唤需要104主站")
	}
	return s.forPeers(func(p *peer) error {
		return p.tracked104(d, ASDU(101, 1, 6, d.CommonAddress, []byte{0, 0, 0, 5}), time.Now())
	})
}
func (p *peer) receivedCommand(a []byte) {
	if len(a) >= 6 && (a[2]&63 == 7 || a[2]&63 == 10) {
		p.mu.Lock()
		delete(p.scheduled, a[0])
		p.mu.Unlock()
	}
}
func cloneASDU(a []byte, cot byte) []byte { b := append([]byte{}, a...); b[2] = cot; return b }
func (p *peer) standardService(d Device, a []byte) (bool, error) {
	if a[0] != 101 && a[0] != 103 {
		return false, nil
	}
	if a[1] != 1 || len(a) < 10 || a[6] != 0 || a[7] != 0 || a[8] != 0 {
		return true, fmt.Errorf("服务命令点位或长度无效")
	}
	if a[0] == 103 {
		if len(a) != 16 {
			return true, fmt.Errorf("校时长度无效")
		}
		stamp, invalid, err := decodeCP56(a[9:])
		if err != nil || invalid {
			return true, p.sendI(cloneASDU(a, 7|64), "校时时间无效")
		}
		p.s.e.log(p.s.cfg.ID, p.address, "SYS", nil, "收到校时（不修改操作系统时间）："+stamp)
		return true, p.sendI(cloneASDU(a, 7), "校时确认")
	}
	if len(a) != 10 || a[9]&63 != 5 {
		return true, p.sendI(cloneASDU(a, 7|64), "脉冲召唤组不支持")
	}
	if err := p.sendI(cloneASDU(a, 7), "脉冲召唤确认"); err != nil {
		return true, err
	}
	points := []Point{}
	for _, point := range d.Points {
		if point.Kind == "mc" {
			points = append(points, point)
		}
	}
	if err := p.publishPoints(d, points, 37); err != nil {
		return true, err
	}
	return true, p.sendI(cloneASDU(a, 10), "脉冲召唤结束")
}

// A Modbus clock is seven bytes of CP56 data held in four registers. The final
// padding byte is zero; the configured broadcast address is used for serial RTU.
func modbusClockPDU(d Device, now time.Time) []byte {
	addr, _ := parseAddress(value(d, "TimeAddr", "1000H"), 65535)
	b := []byte{16, byte(addr >> 8), byte(addr), 0, 4, 8}
	b = append(b, encodeCP56(now)...)
	return append(b, 0)
}
