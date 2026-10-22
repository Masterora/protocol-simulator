package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.bug.st/serial"
)

type session struct {
	clockOffsets map[string]time.Duration
	e            *Engine
	cfg          Channel
	mu           sync.Mutex
	peers        map[*peer]bool
	listener     net.Listener
	udp          *net.UDPConn
	closed       bool
	done         chan struct{}
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}
type pendingRequest struct {
	purpose  string
	deviceID string
	unit     byte
	pdu      []byte
	until    time.Time
}
type iJob struct {
	asdu    []byte
	summary string
	ctx     context.Context
}
type sentI struct {
	sequence uint16
	at       time.Time
}
type peer struct {
	scheduled        map[byte]scheduledCommand
	nextRun          map[string]time.Time
	pollIndex        map[string]int
	delayed          []delayedASDU
	rxConfirmed      uint16
	ackSince         time.Time
	s                *session
	conn             io.ReadWriteCloser
	address          string
	mu               sync.Mutex
	closed           bool
	transfer         bool
	tx, rx, acked    uint16
	lastRX           time.Time
	testSent         time.Time
	startSent        time.Time
	unacked          []sentI
	queue            []iJob
	transaction      uint16
	pending          map[uint16]pendingRequest
	control          *controlWait
	clientSelections map[controlKey]controlSelection
	serverSelections map[controlKey]controlSelection
	controlUncertain bool
	batchOwner       string
	done             chan struct{}
	readDone         chan struct{}
}

func newSession(e *Engine, c Channel) *session {
	return &session{e: e, cfg: c, peers: map[*peer]bool{}, done: make(chan struct{})}
}
func (s *session) open() error {
	switch s.cfg.Transport {
	case "tcp-server":
		l, err := net.Listen("tcp", s.cfg.Address)
		if err != nil {
			return err
		}
		s.listener = l
		s.e.state(s, func(v *ChannelState) { v.Status = "listening"; v.Address = l.Addr().String() })
		s.wg.Add(1)
		go s.accept()
	case "tcp-client":
		dialer := net.Dialer{Timeout: time.Duration(s.cfg.TimeoutMS) * time.Millisecond}
		if s.cfg.LocalAddress != "" {
			addr, err := net.ResolveTCPAddr("tcp", s.cfg.LocalAddress)
			if err != nil {
				return err
			}
			dialer.LocalAddr = addr
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.wg.Add(1)
		go s.connect(ctx, &dialer)
	case "udp":
		remote, err := net.ResolveUDPAddr("udp", s.cfg.Address)
		if err != nil {
			return err
		}
		local := &net.UDPAddr{}
		if s.cfg.LocalAddress != "" {
			local, err = net.ResolveUDPAddr("udp", s.cfg.LocalAddress)
			if err != nil {
				return err
			}
		}
		c, err := net.DialUDP("udp", local, remote)
		if err != nil {
			return err
		}
		s.udp = c
		s.addPeer(c, remote.String())
	case "serial":
		parity := serial.NoParity
		if s.cfg.Parity == "E" {
			parity = serial.EvenParity
		}
		if s.cfg.Parity == "O" {
			parity = serial.OddParity
		}
		stop := serial.OneStopBit
		if s.cfg.StopBits == 2 {
			stop = serial.TwoStopBits
		}
		port, err := serial.Open(s.cfg.Address, &serial.Mode{BaudRate: s.cfg.Baud, DataBits: s.cfg.DataBits, Parity: parity, StopBits: stop})
		if err != nil {
			return err
		}
		s.addPeer(port, s.cfg.Address)
	default:
		return errors.New("不支持的通道")
	}
	s.e.log(s.cfg.ID, "", "SYS", nil, "通道已启动")
	return nil
}

// A client remains active until explicitly stopped, including while its remote
// endpoint is unavailable. Dial cancellation also makes shutdown immediate.
func (s *session) connect(ctx context.Context, dialer *net.Dialer) {
	defer s.wg.Done()
	lastError := ""
	for {
		if ctx.Err() != nil {
			return
		}
		s.e.state(s, func(v *ChannelState) { v.Status = "connecting"; v.Peers = 0 })
		c, err := dialer.DialContext(ctx, "tcp", s.cfg.Address)
		if ctx.Err() != nil {
			if c != nil {
				c.Close()
			}
			return
		}
		if err != nil {
			s.e.state(s, func(v *ChannelState) { v.LastError = err.Error() })
			if lastError != err.Error() {
				s.e.log(s.cfg.ID, "", "SYS", nil, "等待连接，自动重试："+err.Error())
				lastError = err.Error()
			}
		} else {
			lastError = ""
			p := s.addPeer(c, c.RemoteAddr().String())
			if p == nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-p.readDone:
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *session) accept() {
	defer s.wg.Done()
	for {
		c, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				s.e.log(s.cfg.ID, "", "ERR", nil, err.Error())
				return
			}
		}
		s.mu.Lock()
		full := len(s.peers) >= 8
		s.mu.Unlock()
		if full {
			c.Close()
			s.e.log(s.cfg.ID, c.RemoteAddr().String(), "ERR", nil, "已达到 8 个连接上限")
			continue
		}
		s.addPeer(c, c.RemoteAddr().String())
	}
}
func (s *session) addPeer(c io.ReadWriteCloser, address string) *peer {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		c.Close()
		return nil
	}
	p := &peer{s: s, conn: c, address: address, lastRX: time.Now(), pending: map[uint16]pendingRequest{}, done: make(chan struct{}), readDone: make(chan struct{})}
	s.peers[p] = true
	n := len(s.peers)
	s.wg.Add(2)
	s.mu.Unlock()
	s.e.state(s, func(v *ChannelState) { v.Status = "connected"; v.Peers = n; v.LastError = "" })
	s.e.log(s.cfg.ID, address, "SYS", nil, "传输连接已建立")
	go p.readLoop()
	go p.tick()
	if s.cfg.Protocol == "iec104" && s.cfg.Role == "client" {
		p.mu.Lock()
		p.startSent = time.Now()
		err := p.writeLocked(UFrame(StartAct), "STARTDT 激活")
		p.mu.Unlock()
		if err != nil {
			p.fail(err)
		}
	}
	return p
}
func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	peers := make([]*peer, 0, len(s.peers))
	for p := range s.peers {
		peers = append(peers, p)
	}
	s.mu.Unlock()
	for _, p := range peers {
		p.close()
	}
	s.wg.Wait()
}
func (p *peer) close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.done)
	p.conn.Close()
	p.mu.Unlock()
}
func (p *peer) fail(err error) {
	p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, err.Error())
	p.s.e.state(p.s, func(v *ChannelState) { v.LastError = err.Error() })
	p.close()
}
func (s *session) forPeers(fn func(*peer) error) error {
	s.mu.Lock()
	list := make([]*peer, 0, len(s.peers))
	for p := range s.peers {
		list = append(list, p)
	}
	s.mu.Unlock()
	if len(list) == 0 {
		return errors.New("没有已建立的连接")
	}
	for _, p := range list {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}
func (s *session) broadcastRaw(b []byte) error {
	return s.forPeers(func(p *peer) error { p.mu.Lock(); defer p.mu.Unlock(); return p.writeLocked(b, "手动发送") })
}
func (p *peer) writeLocked(b []byte, summary string) error {
	if p.closed {
		return errors.New("连接已关闭")
	}
	if c, ok := p.conn.(net.Conn); ok {
		_ = c.SetWriteDeadline(time.Now().Add(time.Duration(p.s.cfg.TimeoutMS) * time.Millisecond))
	}
	written := 0
	for written < len(b) {
		n, err := p.conn.Write(b[written:])
		written += n
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	if p.s.cfg.Protocol == "iec104" && len(b) >= 6 && b[2]&3 != 3 {
		p.rxConfirmed = binary.LittleEndian.Uint16(b[4:]) >> 1
		p.ackSince = time.Time{}
	}
	p.s.e.log(p.s.cfg.ID, p.address, "TX", b, summary)
	return nil
}
func (p *peer) readLoop() {
	defer p.s.wg.Done()
	defer func() {
		p.close()
		s := p.s
		s.mu.Lock()
		delete(s.peers, p)
		n := len(s.peers)
		s.mu.Unlock()
		s.e.state(s, func(v *ChannelState) {
			v.Peers = n
			if n == 0 {
				if s.cfg.Transport == "tcp-server" {
					v.Status = "listening"
				} else if s.cfg.Transport == "tcp-client" {
					v.Status = "connecting"
				} else {
					v.Status = "disconnected"
				}
			}
		})
		s.e.log(s.cfg.ID, p.address, "SYS", nil, "连接已断开")
		if p.readDone != nil {
			close(p.readDone)
		}
	}()
	r := bufio.NewReader(p.conn)
	for {
		p.clearReadDeadline()
		frame := &packetReader{Reader: r, peer: p}
		var b []byte
		var err error
		switch p.s.cfg.Protocol {
		case "modbus-tcp":
			b, err = ReadModbusTCP(frame)
		case "modbus-rtu":
			b, err = readModbusRTU(frame, p.s.cfg.Role == "server", func(unit byte) bool {
				for _, d := range p.s.e.devices(p.s.cfg.ID) {
					if unit == 0 || d.Unit == unit {
						return highCRC(d)
					}
				}
				return false
			})
		case "iec104":
			b, err = Read104(frame)
		default:
			buf := make([]byte, 4096)
			var n int
			n, err = r.Read(buf)
			b = buf[:n]
		}
		if err != nil {
			select {
			case <-p.done:
				return
			default:
			}
			if err != io.EOF {
				p.fail(err)
			}
			return
		}
		if len(b) == 0 {
			continue
		}
		p.mu.Lock()
		p.lastRX = time.Now()
		p.mu.Unlock()
		p.s.e.log(p.s.cfg.ID, p.address, "RX", b, describe(p.s.cfg.Protocol, b))
		switch p.s.cfg.Protocol {
		case "modbus-tcp", "modbus-rtu":
			err = p.handleModbus(b)
		case "iec104":
			err = p.handle104(b)
		}
		if err != nil {
			p.fail(err)
			return
		}
	}
}
func describe(protocol string, b []byte) string {
	switch protocol {
	case "modbus-tcp":
		if len(b) > 7 {
			return fmt.Sprintf("Modbus TCP · TID=%d · FC=%02X", binary.BigEndian.Uint16(b), b[7])
		}
	case "modbus-rtu":
		if len(b) > 1 {
			return fmt.Sprintf("Modbus RTU · 地址=%d · FC=%02X", b[0], b[1])
		}
	case "iec104":
		if len(b) >= 6 {
			if b[2]&1 == 0 {
				if len(b) >= 12 {
					return fmt.Sprintf("I帧 · S=%d R=%d · 类型=%d 原因=%d", binary.LittleEndian.Uint16(b[2:])>>1, binary.LittleEndian.Uint16(b[4:])>>1, b[6], b[8]&0x3f)
				}
			} else if b[2]&3 == 1 {
				return fmt.Sprintf("S帧 · R=%d", binary.LittleEndian.Uint16(b[4:])>>1)
			} else {
				return fmt.Sprintf("U帧 · 控制=%02X", b[2])
			}
		}
	}
	return fmt.Sprintf("%d 字节", len(b))
}

func (s *session) requestModbus(pdu []byte) error {
	d, err := s.e.Device(s.cfg.ID, "")
	if err != nil {
		return err
	}
	return s.requestDeviceModbus(d, pdu)
}
func (s *session) requestDeviceModbus(d Device, pdu []byte) error {
	return s.requestDeviceModbusMode(d, pdu, "")
}
func (s *session) requestDeviceModbusMode(d Device, pdu []byte, purpose string) error {
	if (s.cfg.Protocol != "modbus-tcp" && s.cfg.Protocol != "modbus-rtu") || s.cfg.Role != "client" {
		return errors.New("此操作需要 Modbus 主站")
	}
	if s.cfg.Protocol == "modbus-rtu" && d.Unit == 0 && pdu[0] <= 4 {
		return errors.New("RTU 广播地址不能读取数据")
	}
	return s.forPeers(func(p *peer) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		if len(p.pending) >= 32 || (s.cfg.Protocol == "modbus-rtu" && len(p.pending) > 0) {
			return errors.New("已有请求等待应答，请稍后重试")
		}
		for {
			p.transaction++
			if _, exists := p.pending[p.transaction]; !exists {
				break
			}
		}
		tid := p.transaction
		b := EncodeTCP(tid, d.Unit, pdu)
		if s.cfg.Protocol == "modbus-rtu" {
			tid = 0
			b = encodeDeviceRTU(d.Unit, pdu, d)
			if d.Unit == 0 {
				return p.writeLocked(b, "RTU 广播写入（不等待应答）")
			}
		}
		p.pending[tid] = pendingRequest{purpose: purpose, deviceID: d.ID, unit: d.Unit, pdu: append([]byte{}, pdu...), until: time.Now().Add(time.Duration(number(d, "txt_overTime", number(d, "txtTimeOut", s.cfg.TimeoutMS/1000))) * time.Second)}
		if err := p.writeLocked(b, "Modbus 请求 · "+d.Name); err != nil {
			delete(p.pending, tid)
			return err
		}
		return nil
	})
}
func (p *peer) handleModbus(b []byte) error {
	s := p.s
	tid := uint16(0)
	unit := b[0]
	pdu := b[1 : len(b)-2]
	if s.cfg.Protocol == "modbus-tcp" {
		tid = binary.BigEndian.Uint16(b)
		unit = b[6]
		pdu = b[7:]
	}
	if s.cfg.Role == "server" {
		broadcast := s.cfg.Protocol == "modbus-rtu" && unit == 0
		if broadcast && pdu[0] <= 4 {
			return nil
		}
		for _, d := range s.e.devices(s.cfg.ID) {
			if d.Unit != unit && !broadcast {
				continue
			}
			resolved, lookupErr := s.e.Device(s.cfg.ID, d.ID)
			if lookupErr != nil {
				continue
			}
			d = resolved
			reply, updates := ModbusDeviceReply(pdu, d)
			if clockReply, handled := p.modbusClock(d, pdu); handled {
				reply = clockReply
				updates = nil
			}
			s.e.applyDevicePoints(s.cfg.ID, d.ID, updates)
			if reply == nil || broadcast {
				continue
			}
			out := EncodeTCP(tid, unit, reply)
			if s.cfg.Protocol == "modbus-rtu" {
				out = encodeDeviceRTU(unit, reply, d)
			}
			p.mu.Lock()
			err := p.writeLocked(out, "Modbus 子站应答 · "+d.Name)
			p.mu.Unlock()
			return err
		}
		return nil
	}
	p.mu.Lock()
	request, ok := p.pending[tid]
	if ok && unit == request.unit {
		delete(p.pending, tid)
	}
	p.mu.Unlock()
	if !ok {
		s.e.log(s.cfg.ID, p.address, "ERR", nil, "忽略未匹配事务的应答")
		return nil
	}
	if unit != request.unit {
		s.e.log(s.cfg.ID, p.address, "ERR", nil, "Modbus 设备地址不匹配")
		return nil
	}
	if len(request.pdu) >= 6 && request.pdu[0] == 16 {
		if len(pdu) == 5 && bytes.Equal(request.pdu[:5], pdu) {
			s.e.log(s.cfg.ID, p.address, "SYS", nil, "校时写入确认")
			return nil
		}
		return nil
	}
	dForFormat, _ := s.e.Device(s.cfg.ID, request.deviceID)
	pdu = bitOrderPDU(pdu, dForFormat, false)
	points, err := DecodeModbusResponse(request.pdu, pdu)
	if err != nil {
		s.e.log(s.cfg.ID, p.address, "ERR", nil, err.Error())
		return nil
	}
	d, lookupErr := s.e.Device(s.cfg.ID, request.deviceID)
	if lookupErr == nil && d.Unit == request.unit {
		clockAddr, _ := parseAddress(value(d, "TimeAddr", "1000H"), 65535)
		if request.purpose == "clock" && len(request.pdu) == 5 && uint32(binary.BigEndian.Uint16(request.pdu[1:])) == clockAddr && len(pdu) == 10 {
			stamp, _, decodeErr := decodeCP56(pdu[2:9])
			if decodeErr == nil {
				s.e.log(s.cfg.ID, p.address, "SYS", nil, "装置时间："+stamp)
			}
			return nil
		}
		if flag(d, "cb_ReadEvent", false) {
			eventAddr, _ := parseAddress(value(d, "txtEventAddr", "500H"), 65535)
			if uint32(binary.BigEndian.Uint16(request.pdu[1:])) == eventAddr {
				s.e.recordEvents(s.cfg.ID, d.ID, points, 3)
			}
		}
		mapped, err := collapseRegisters(d, points, false)
		if err != nil {
			s.e.log(s.cfg.ID, p.address, "ERR", nil, err.Error())
		} else {
			s.e.applyDevicePoints(s.cfg.ID, d.ID, mapped)
		}
	}
	return nil
}

func (p *peer) sendI(asdu []byte, summary string) error {
	return p.sendIContext(context.Background(), asdu, summary)
}
func (p *peer) sendIContext(ctx context.Context, asdu []byte, summary string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.transfer {
		return errors.New("104 数据传输尚未激活，请等待 STARTDT 确认")
	}
	if len(asdu) < 6 {
		return errors.New("ASDU 编码无效")
	}
	if len(asdu) > 249 {
		return errors.New("ASDU 过长")
	}
	if len(p.queue) >= 16384 {
		return errors.New("104 发送队列已满")
	}
	p.queue = append(p.queue, iJob{asdu: asdu, summary: summary, ctx: ctx})
	return p.flushLocked()
}
func (p *peer) flushLocked() error {
	for p.transfer && len(p.queue) > 0 && len(p.unacked) < sendWindow104(p.device104()) {
		job := p.queue[0]
		p.queue = p.queue[1:]
		if job.ctx != nil && job.ctx.Err() != nil {
			continue
		}
		wire, err := p.wireASDU(job.asdu)
		if err != nil {
			return err
		}
		if err := p.writeLocked(IFrame(p.tx, p.rx, wire), job.summary); err != nil {
			return err
		}
		p.unacked = append(p.unacked, sentI{sequence: p.tx, at: time.Now()})
		p.tx = (p.tx + 1) & 0x7fff
	}
	return nil
}
func (p *peer) ackLocked(ack uint16) error {
	advance := (ack - p.acked) & 0x7fff
	outstanding := (p.tx - p.acked) & 0x7fff
	if advance > outstanding {
		return errors.New("104 确认序号超出已发送范围")
	}
	if int(advance) > len(p.unacked) {
		return errors.New("104 确认窗口不一致")
	}
	p.unacked = p.unacked[int(advance):]
	p.acked = ack
	return p.flushLocked()
}
func (p *peer) handle104(b []byte) error {
	if b[2]&3 == 3 {
		if len(b) != 6 || b[3] != 0 || b[4] != 0 || b[5] != 0 {
			return errors.New("104 U帧长度或保留字节无效")
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		switch b[2] {
		case StartAct:
			p.transfer = true
			if err := p.writeLocked(UFrame(StartCon), "STARTDT 确认"); err != nil {
				return err
			}
		case StartCon:
			p.transfer = true
			p.startSent = time.Time{}
		case StopAct:
			p.abortControlLocked()
			p.transfer = false
			p.queue = nil
			return p.writeLocked(UFrame(StopCon), "STOPDT 确认")
		case StopCon:
			p.abortControlLocked()
			p.transfer = false
			p.queue = nil
		case TestAct:
			return p.writeLocked(UFrame(TestCon), "TESTFR 确认")
		case TestCon:
			p.testSent = time.Time{}
		default:
			return errors.New("不支持的 104 U帧")
		}
		return nil
	}
	p.mu.Lock()
	if b[2]&3 == 1 {
		if len(b) != 6 || b[2] != 1 || b[3] != 0 || b[4]&1 != 0 {
			p.mu.Unlock()
			return errors.New("104 S帧格式无效")
		}
		err := p.ackLocked(binary.LittleEndian.Uint16(b[4:]) >> 1)
		p.mu.Unlock()
		return err
	}
	if len(b) < 9 || b[2]&1 != 0 || b[4]&1 != 0 {
		p.mu.Unlock()
		return errors.New("104 I帧格式无效")
	}
	if !p.transfer {
		p.mu.Unlock()
		return errors.New("104 数据传输未激活")
	}
	send := binary.LittleEndian.Uint16(b[2:]) >> 1
	if send != p.rx {
		p.mu.Unlock()
		return fmt.Errorf("104 发送序号不连续：收到 %d，期望 %d", send, p.rx)
	}
	p.rx = (p.rx + 1) & 0x7fff
	if err := p.ackLocked(binary.LittleEndian.Uint16(b[4:]) >> 1); err != nil {
		p.mu.Unlock()
		return err
	}
	d104 := p.device104()
	limit := number(d104, "text_replySICount", number(d104, "txt_replyIFrame", 1))
	if p.ackSince.IsZero() {
		p.ackSince = time.Now()
	}
	var err error
	if int((p.rx-p.rxConfirmed)&0x7fff) >= limit {
		err = p.writeLocked(SFrame(p.rx), "I帧确认")
	}
	p.mu.Unlock()
	if err != nil {
		return err
	}
	canonical, err := p.canonicalASDU(b[6:])
	if err != nil {
		return err
	}
	return p.handleASDU(canonical, false)
}
func (p *peer) handleASDU(a []byte, delayed bool) error {
	s := p.s
	ca := binary.LittleEndian.Uint16(a[4:])
	var d Device
	found := false
	for _, candidate := range s.e.devices(s.cfg.ID) {
		if candidate.CommonAddress == ca {
			d = candidate
			found = true
			break
		}
	}
	if !found {
		if s.cfg.Role == "server" && controlKind(a[0]) != "" {
			reply := append([]byte{}, a...)
			reply[2] = 46 | 0x40
			return p.sendI(reply, "控制公共地址不存在")
		}
		s.e.log(s.cfg.ID, p.address, "ERR", nil, "公共地址不匹配，已忽略 ASDU")
		return nil
	}
	resolved, lookupErr := s.e.Device(s.cfg.ID, d.ID)
	if lookupErr != nil {
		return nil
	}
	d = resolved
	if s.cfg.Role == "server" && (a[2]&63 == 6 || a[2]&63 == 8) && !delayed {
		mode := value(d, "responseMode", "normal")
		if mode == "silent" {
			return nil
		}
		if mode == "deny" {
			cot := byte(7 | 64)
			if a[2]&63 == 8 {
				cot = 9 | 64
			}
			return p.sendI(cloneASDU(a, cot), "配置的否认应答")
		}
		delay := number(d, "responseDelayMs", 0)
		if delay > 0 {
			p.mu.Lock()
			if len(p.delayed) >= 256 {
				p.mu.Unlock()
				return fmt.Errorf("延迟应答队列已满")
			}
			p.delayed = append(p.delayed, delayedASDU{append([]byte{}, a...), time.Now().Add(time.Duration(delay) * time.Millisecond)})
			p.mu.Unlock()
			return nil
		}
	}
	if s.cfg.Role == "client" {
		p.receivedCommand(a)
	}
	if controlKind(a[0]) != "" {
		if s.cfg.Role == "client" {
			return p.controlReply104(a)
		}
		return p.controlRequest104(d, a)
	}
	if s.cfg.Role == "client" {
		points, err := Decode104Points(a, d.CommonAddress)
		if err != nil {
			return err
		}
		s.e.recordEvents(s.cfg.ID, d.ID, points, a[2]&63)
		s.e.applyDevicePoints(s.cfg.ID, d.ID, points)
		return nil
	}
	if a[2]&63 == 6 {
		if handled, err := p.standardService(d, a); handled {
			return err
		}
	}
	if a[0] == 100 && a[2]&0x3f == 6 {
		if len(a) != 10 || a[1] != 1 || a[6] != 0 || a[7] != 0 || a[8] != 0 || a[9] != 20 {
			return errors.New("本版仅支持全站总召唤 QOI=20")
		}
		if err := p.sendI(interrogation(d.CommonAddress, 7), "总召唤激活确认"); err != nil {
			return err
		}
		points := []Point{}
		for _, point := range d.Points {
			if point.Kind == "yx" || point.Kind == "yc" || point.Kind == "position" {
				points = append(points, point)
			}
		}
		if err := p.publishPoints(d, points, 20); err != nil {
			return err
		}
		return p.sendI(interrogation(d.CommonAddress, 10), "总召唤结束")
	}
	// Unimplemented control and extension ASDUs receive a negative unknown-type response.
	if a[2]&0x3f == 6 {
		reply := append([]byte{}, a...)
		reply[2] = 44 | 0x40
		return p.sendI(reply, "不支持的 ASDU 类型")
	}
	return nil
}

func (p *peer) tick() {
	defer p.s.wg.Done()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case now := <-ticker.C:
			p.mu.Lock()
			var err error
			for tid, req := range p.pending {
				if now.After(req.until) {
					delete(p.pending, tid)
					p.s.e.log(p.s.cfg.ID, p.address, "ERR", nil, fmt.Sprintf("Modbus 请求超时 · TID=%d", tid))
				}
			}
			if p.s.cfg.Protocol == "iec104" {
				timeout := time.Duration(p.s.cfg.TimeoutMS) * time.Millisecond
				if len(p.unacked) > 0 && now.Sub(p.unacked[0].at) > timeout {
					err = errors.New("104 I帧确认超时")
				}
				if !p.startSent.IsZero() && now.Sub(p.startSent) > timeout {
					err = errors.New("104 STARTDT 确认超时")
				}
				if !p.testSent.IsZero() && now.Sub(p.testSent) > timeout {
					err = errors.New("104 TESTFR 确认超时")
				}
				if err == nil && p.transfer && now.Sub(p.lastRX) > seconds(p.device104(), "txtUTest_Time", 20) && p.testSent.IsZero() {
					p.testSent = now
					err = p.writeLocked(UFrame(TestAct), "TESTFR 激活")
				}
			}
			if err == nil && p.s.cfg.Protocol == "iec104" && p.rxConfirmed != p.rx && !p.ackSince.IsZero() && now.Sub(p.ackSince) >= time.Duration(number(p.device104(), "ackDelayMs", 1000))*time.Millisecond {
				err = p.writeLocked(SFrame(p.rx), "延迟S帧确认")
			}
			due := []delayedASDU{}
			kept := p.delayed[:0]
			for _, job := range p.delayed {
				if !now.Before(job.at) {
					due = append(due, job)
				} else {
					kept = append(kept, job)
				}
			}
			p.delayed = kept
			p.mu.Unlock()
			for _, job := range due {
				if processErr := p.handleASDU(job.asdu, true); processErr != nil {
					err = processErr
					break
				}
			}
			if err == nil {
				p.periodic(now)
			}
			if err != nil {
				p.fail(err)
				return
			}
		}
	}
}

func SerialPorts() ([]string, error) { return serial.GetPortsList() }
