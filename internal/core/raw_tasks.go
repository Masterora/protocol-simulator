package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RawReplyRule struct {
	Enabled  bool   `json:"enabled"`
	Offset   int    `json:"offset"`
	Match    string `json:"match"`
	Response string `json:"response"`
	Format   string `json:"format"`
	Checksum string `json:"checksum"`
	DelayMS  int    `json:"delayMs"`
}
type RawSendRow struct {
	Enabled  bool   `json:"enabled"`
	Data     string `json:"data"`
	Format   string `json:"format"`
	Checksum string `json:"checksum"`
}
type RawToolsConfig struct {
	Framing     string         `json:"framing"`
	FrameLength int            `json:"frameLength"`
	Delimiter   string         `json:"delimiter"`
	IdleMS      int            `json:"idleMs"`
	Replies     []RawReplyRule `json:"replies"`
	Rows        []RawSendRow   `json:"rows"`
	Selection   string         `json:"selection"`
	IntervalMS  int            `json:"intervalMs"`
	CycleMS     int            `json:"cycleMs"`
	Cycles      int            `json:"cycles"`
}
type RawTaskStatus struct {
	Running bool   `json:"running"`
	Sent    uint64 `json:"sent"`
	Cycles  uint64 `json:"cycles"`
	Error   string `json:"error"`
}
type RawToolsView struct {
	Config RawToolsConfig `json:"config"`
	Reply  RawTaskStatus  `json:"reply"`
	Send   RawTaskStatus  `json:"send"`
}
type rawTaskKey struct{ channel, mode string }

type rawInput struct {
	peer *peer
	data []byte
}
type rawTask struct {
	mu      sync.Mutex
	status  RawTaskStatus
	session *session
	cfg     RawToolsConfig
	cancel  context.CancelFunc
	done    chan struct{}
	input   chan rawInput
}

func cloneRawTools(c *RawToolsConfig) *RawToolsConfig {
	if c == nil {
		return nil
	}
	v := *c
	v.Replies = append([]RawReplyRule{}, c.Replies...)
	v.Rows = append([]RawSendRow{}, c.Rows...)
	return &v
}
func defaultRawTools() RawToolsConfig {
	return RawToolsConfig{Framing: "idle", IdleMS: 30, IntervalMS: 1000, Cycles: 1, Replies: []RawReplyRule{}, Rows: []RawSendRow{}}
}
func rawPayload(text, format, checksum string) ([]byte, error) {
	var b []byte
	switch format {
	case "", "hex":
		clean := strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "", ":", "").Replace(text)
		var err error
		b, err = hex.DecodeString(clean)
		if err != nil {
			return nil, errors.New("报文应为完整十六进制字节对")
		}
	case "text":
		b = []byte(text)
	default:
		return nil, errors.New("报文格式应为hex或text（UTF-8）")
	}
	if len(b) == 0 || len(b) > 4096 {
		return nil, errors.New("每条报文须包含1–4096字节")
	}
	switch checksum {
	case "", "none":
	case "crc16", "crc16-high":
		n := CRC16(b)
		if checksum == "crc16-high" {
			b = append(b, byte(n>>8), byte(n))
		} else {
			b = append(b, byte(n), byte(n>>8))
		}
	case "sum":
		var n byte
		for _, v := range b {
			n += v
		}
		b = append(b, n)
	case "xor":
		var n byte
		for _, v := range b {
			n ^= v
		}
		b = append(b, n)
	default:
		return nil, errors.New("不支持的校验方式")
	}
	return b, nil
}
func validateRawTools(c RawToolsConfig) error {
	switch c.Framing {
	case "idle":
		if c.IdleMS < 5 || c.IdleMS > 5000 {
			return errors.New("帧间静默时间应为5–5000毫秒")
		}
	case "fixed":
		if c.FrameLength < 1 || c.FrameLength > 4096 {
			return errors.New("固定帧长应为1–4096字节")
		}
	case "delimiter":
		b, err := hex.DecodeString(strings.ReplaceAll(c.Delimiter, " ", ""))
		if err != nil || len(b) < 1 || len(b) > 32 {
			return errors.New("结束符须为1–32字节的十六进制报文")
		}
	case "chunk":
	default:
		return errors.New("请选择按静默、固定长度、结束符或接收块分帧")
	}
	if len(c.Replies) > 256 || len(c.Rows) > 256 {
		return errors.New("自动应答与发送列表各最多256条")
	}
	for i, r := range c.Replies {
		if !r.Enabled {
			continue
		}
		match, err := hex.DecodeString(strings.ReplaceAll(r.Match, " ", ""))
		if err != nil || len(match) == 0 || r.Offset < 0 || r.Offset > 4096-len(match) {
			return fmt.Errorf("第%d条应答的识别范围无效", i+1)
		}
		if r.DelayMS < 0 || r.DelayMS > 60000 {
			return errors.New("应答延迟应为0–60000毫秒")
		}
		if _, err = rawPayload(r.Response, r.Format, r.Checksum); err != nil {
			return fmt.Errorf("第%d条应答：%w", i+1, err)
		}
	}
	for i, r := range c.Rows {
		if !r.Enabled {
			continue
		}
		if _, err := rawPayload(r.Data, r.Format, r.Checksum); err != nil {
			return fmt.Errorf("第%d条发送：%w", i+1, err)
		}
	}
	if c.IntervalMS < 10 || c.IntervalMS > 86400000 || c.CycleMS < 0 || c.CycleMS > 86400000 || c.Cycles < 0 || c.Cycles > 1000000 {
		return errors.New("发送间隔应为10毫秒–24小时，循环间隔为0–24小时，次数为0–1000000（0为持续）")
	}
	if c.Selection != "" {
		if _, err := rawSelection(c); err != nil {
			return err
		}
	}
	return nil
}
func rawSelection(c RawToolsConfig) ([]int, error) {
	var indices []int
	if strings.TrimSpace(c.Selection) == "" {
		for i, r := range c.Rows {
			if r.Enabled {
				indices = append(indices, i)
			}
		}
	} else {
		if len(c.Selection) > 2048 {
			return nil, errors.New("指定序号过长")
		}
		for _, token := range strings.Split(strings.ReplaceAll(c.Selection, "，", ","), ",") {
			parts := strings.Split(strings.TrimSpace(token), "-")
			if len(parts) > 2 {
				return nil, errors.New("序号用逗号分隔，范围使用-")
			}
			first, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil || first < 1 || first > len(c.Rows) {
				return nil, errors.New("发送序号超出列表范围（从1开始）")
			}
			last := first
			if len(parts) == 2 {
				last, err = strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil || last < first || last > len(c.Rows) {
					return nil, errors.New("发送序号范围无效")
				}
			}
			for n := first; n <= last; n++ {
				if c.Rows[n-1].Enabled {
					indices = append(indices, n-1)
					if len(indices) > 256 {
						return nil, errors.New("一次最多选择256条报文")
					}
				}
			}
		}
	}
	return indices, nil
}
func (e *Engine) GetRawTools(channel string) (RawToolsView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out RawToolsView
	found := false
	for _, c := range e.project.Channels {
		if c.ID == channel {
			if c.Protocol != "raw" {
				return out, errors.New("报文自动化仅适用于RAW通道")
			}
			out.Config = defaultRawTools()
			if c.RawTools != nil {
				out.Config = *cloneRawTools(c.RawTools)
			}
			found = true
			break
		}
	}
	if !found {
		return out, errors.New("通道不存在")
	}
	if j := e.rawTasks[rawTaskKey{channel, "reply"}]; j != nil {
		j.mu.Lock()
		out.Reply = j.status
		j.mu.Unlock()
	}
	if j := e.rawTasks[rawTaskKey{channel, "send"}]; j != nil {
		j.mu.Lock()
		out.Send = j.status
		j.mu.Unlock()
	}
	return out, nil
}
func (e *Engine) SaveRawTools(channel string, cfg RawToolsConfig) error {
	if err := validateRawTools(cfg); err != nil {
		return err
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, mode := range []string{"reply", "send"} {
		if j := e.rawTasks[rawTaskKey{channel, mode}]; j != nil {
			j.mu.Lock()
			running := j.status.Running
			j.mu.Unlock()
			if running {
				return errors.New("请先停止自动应答和循环发送，再修改配置")
			}
		}
	}
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID == channel {
			if c.Protocol != "raw" {
				return errors.New("报文自动化仅适用于RAW通道")
			}
			c.RawTools = cloneRawTools(&cfg)
			e.dirty = true
			e.markConfigurationLocked()
			return nil
		}
	}
	return errors.New("通道不存在")
}
func (e *Engine) StartRawTask(channel, mode string) error {
	if mode != "reply" && mode != "send" {
		return errors.New("请选择自动应答或循环发送")
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.running[channel]
	if s == nil || s.cfg.Protocol != "raw" {
		return errors.New("请先连接RAW通道")
	}
	var cfg *RawToolsConfig
	for _, c := range e.project.Channels {
		if c.ID == channel {
			cfg = cloneRawTools(c.RawTools)
		}
	}
	if cfg == nil {
		return errors.New("请先保存报文自动化配置")
	}
	if err := validateRawTools(*cfg); err != nil {
		return err
	}
	if mode == "reply" {
		count := 0
		for _, r := range cfg.Replies {
			if r.Enabled {
				count++
			}
		}
		if count == 0 {
			return errors.New("请至少启用一条应答规则")
		}
	} else {
		indices, err := rawSelection(*cfg)
		if err != nil {
			return err
		}
		if len(indices) == 0 {
			return errors.New("没有可发送的已启用报文")
		}
	}
	key := rawTaskKey{channel, mode}
	if old := e.rawTasks[key]; old != nil {
		old.mu.Lock()
		running := old.status.Running
		old.mu.Unlock()
		if running {
			return errors.New("任务已在运行")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &rawTask{status: RawTaskStatus{Running: true}, session: s, cfg: *cfg, cancel: cancel, done: make(chan struct{}), input: make(chan rawInput, 128)}
	if e.rawTasks == nil {
		e.rawTasks = map[rawTaskKey]*rawTask{}
	}
	e.rawTasks[key] = j
	go j.run(ctx, mode)
	return nil
}
func (e *Engine) StopRawTask(channel, mode string) error {
	if mode != "reply" && mode != "send" {
		return errors.New("无效任务")
	}
	e.mu.Lock()
	j := e.rawTasks[rawTaskKey{channel, mode}]
	e.mu.Unlock()
	if j == nil {
		return nil
	}
	j.cancel()
	select {
	case <-j.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("发送尚未结束，请等待网络写入超时")
	}
}
func (e *Engine) cancelRawTasksLocked(channel string) {
	for key, j := range e.rawTasks {
		if key.channel == channel {
			j.cancel()
		}
	}
}
func (s *session) dispatchRaw(p *peer, b []byte) {
	s.e.mu.Lock()
	j := s.e.rawTasks[rawTaskKey{s.cfg.ID, "reply"}]
	s.e.mu.Unlock()
	if j == nil || j.session != s {
		return
	}
	j.mu.Lock()
	running := j.status.Running
	j.mu.Unlock()
	if !running {
		return
	}
	select {
	case j.input <- rawInput{p, append([]byte{}, b...)}:
	default:
		j.fail(errors.New("自动应答接收队列已满，任务已停止"))
		j.cancel()
	}
}
func (j *rawTask) fail(err error) {
	if err != nil {
		j.mu.Lock()
		j.status.Error = err.Error()
		j.mu.Unlock()
	}
}
func (j *rawTask) send(ctx context.Context, p *peer, b []byte, summary string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-j.session.done:
		return errors.New("通道已停止")
	default:
	}
	if err := p.writeLocked(b, summary); err != nil {
		return err
	}
	j.mu.Lock()
	j.status.Sent++
	j.mu.Unlock()
	return nil
}
func waitRaw(ctx context.Context, s *session, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return errors.New("通道已停止")
	case <-t.C:
		return nil
	}
}
func (j *rawTask) run(ctx context.Context, mode string) {
	defer close(j.done)
	defer func() { j.mu.Lock(); j.status.Running = false; j.mu.Unlock() }()
	var err error
	if mode == "send" {
		err = j.runSend(ctx)
	} else {
		err = j.runReply(ctx)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		j.fail(err)
	}
}
func (j *rawTask) runSend(ctx context.Context) error {
	indices, _ := rawSelection(j.cfg)
	for cycle := 0; j.cfg.Cycles == 0 || cycle < j.cfg.Cycles; cycle++ {
		start := time.Now()
		for n, index := range indices {
			row := j.cfg.Rows[index]
			b, _ := rawPayload(row.Data, row.Format, row.Checksum)
			if err := j.session.forPeers(func(p *peer) error { return j.send(ctx, p, b, "循环发送") }); err != nil {
				return err
			}
			if n < len(indices)-1 {
				if err := waitRaw(ctx, j.session, time.Duration(j.cfg.IntervalMS)*time.Millisecond); err != nil {
					return err
				}
			}
		}
		j.mu.Lock()
		j.status.Cycles++
		j.mu.Unlock()
		if j.cfg.Cycles != 0 && cycle+1 >= j.cfg.Cycles {
			return nil
		}
		delay := time.Duration(j.cfg.IntervalMS) * time.Millisecond
		if remaining := time.Duration(j.cfg.CycleMS)*time.Millisecond - time.Since(start); remaining > delay {
			delay = remaining
		}
		if err := waitRaw(ctx, j.session, delay); err != nil {
			return err
		}
	}
	return nil
}

type rawBuffer struct {
	data []byte
	at   time.Time
}

func (j *rawTask) runReply(ctx context.Context) error {
	buffers := map[*peer]rawBuffer{}
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	process := func(p *peer, frame []byte) error {
		for _, r := range j.cfg.Replies {
			if !r.Enabled {
				continue
			}
			match, _ := hex.DecodeString(strings.ReplaceAll(r.Match, " ", ""))
			if r.Offset+len(match) > len(frame) || !bytes.Equal(frame[r.Offset:r.Offset+len(match)], match) {
				continue
			}
			if r.DelayMS > 0 {
				if err := waitRaw(ctx, j.session, time.Duration(r.DelayMS)*time.Millisecond); err != nil {
					return err
				}
			}
			b, _ := rawPayload(r.Response, r.Format, r.Checksum)
			if err := j.send(ctx, p, b, "自动应答"); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				j.session.e.log(j.session.cfg.ID, p.address, "ERR", nil, "自动应答未发送："+err.Error())
			}
			break
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-j.session.done:
			return nil
		case input := <-j.input:
			p := input.peer
			if j.cfg.Framing == "chunk" || j.session.cfg.Transport == "udp" {
				if err := process(p, input.data); err != nil {
					return err
				}
				continue
			}
			v := buffers[p]
			v.data = append(v.data, input.data...)
			v.at = time.Now()
			if len(v.data) > 65536 {
				return errors.New("接收帧缓存超过64KB，任务已停止")
			}
			for {
				size := 0
				if j.cfg.Framing == "fixed" && len(v.data) >= j.cfg.FrameLength {
					size = j.cfg.FrameLength
				}
				if j.cfg.Framing == "delimiter" {
					delim, _ := hex.DecodeString(strings.ReplaceAll(j.cfg.Delimiter, " ", ""))
					if n := bytes.Index(v.data, delim); n >= 0 {
						size = n + len(delim)
					}
				}
				if size == 0 {
					break
				}
				if err := process(p, v.data[:size]); err != nil {
					return err
				}
				v.data = v.data[size:]
			}
			if len(v.data) == 0 {
				delete(buffers, p)
			} else {
				buffers[p] = v
			}
		case now := <-tick.C:
			for p, v := range buffers {
				p.mu.Lock()
				closed := p.closed
				p.mu.Unlock()
				if closed {
					delete(buffers, p)
					continue
				}
				if j.cfg.Framing == "idle" && now.Sub(v.at) >= time.Duration(j.cfg.IdleMS)*time.Millisecond {
					if err := process(p, v.data); err != nil {
						return err
					}
					delete(buffers, p)
				}
			}
		}
	}
}
