// Package tui observes independent engines and sends only explicit data-point operations. It never owns an engine lifecycle.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"golang.org/x/term"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

var viewNames = []string{"Projects", "Channels", "Devices", "Points", "Rules", "Frames", "Logs"}

type instance struct {
	socket string
	info   observe.Info
	err    error
}
type request struct {
	dir, socket, channel, device, filter string
	kind                                 string
	offset                               int
	after, logAfter                      uint64
}
type update struct {
	request         request
	instances       []instance
	detail          observe.Info
	socket          string
	channel, device string
	kind            string
	err             error
}
type detailTarget struct {
	view                        int
	id, socket, channel, device string
	ruleKey                     string
	ruleIndex                   int
	ruleLayout                  string
	ruleDuplicate               bool
}
type model struct {
	pendingRule                 *ruleOperation
	deleting                    *deleteConfirmation
	picker                      *choicePicker
	pickerLabels                []string
	runtimeDir                  string
	configurationEditor         *configurationEditor
	pendingConfiguration        *configurationOperation
	editor                      *pointEditor
	pendingOperation            *manualOperation
	operationBusy               bool
	operationNotice             string
	operationNoticeAt           time.Time
	detailOpen                  bool
	detailTarget                detailTarget
	detailScroll                int
	detailTitle                 string
	detailLines                 []string
	discovered                  bool
	follow                      bool
	connected                   bool
	view, cursor, pointOffset   int
	socket, channel, device     string
	pointKind                   string
	instances                   []instance
	detail                      observe.Info
	messages                    []core.Message
	logs                        []observe.Log
	after, logAfter             uint64
	filter, input, mode, notice string
	help                        bool
}

func sanitize(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, text)
}
func fit(text string, width int) string {
	text = sanitize(text)
	var out strings.Builder
	used := 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		w := g.Width()
		if used+w > width {
			break
		}
		out.WriteString(g.Str())
		used += w
	}
	return out.String() + strings.Repeat(" ", max(0, width-used))
}

func clipped(text string, width int, tail bool) string {
	text = sanitize(text)
	if width <= 0 {
		return ""
	}
	if uniseg.StringWidth(text) <= width {
		return text
	}
	if !tail {
		return strings.TrimRight(fit(text, width-1), " ") + "…"
	}
	parts := []string{}
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		parts = append(parts, graphemes.Str())
	}
	used, start := 0, len(parts)
	for start > 0 && used+uniseg.StringWidth(parts[start-1]) <= width-1 {
		start--
		used += uniseg.StringWidth(parts[start])
	}
	return "…" + strings.Join(parts[start:], "")
}

// bindProject changes only the viewer's scope; it never touches a running engine.
func (m *model) bindProject(item instance) {
	if m.socket == item.socket {
		return
	}
	m.socket, m.channel, m.device = item.socket, "", ""
	m.detail = item.info
	m.connected = false
	m.messages, m.logs = nil, nil
	m.after, m.logAfter = 0, 0
	m.pointOffset = 0
	m.pointKind = ""
	if len(item.info.View.Channels) > 0 {
		m.channel = item.info.View.Channels[0].ID
	}
}
func (m *model) selectView(view int) {
	rows := m.rows()
	if len(rows) > 0 {
		id := rows[min(m.cursor, len(rows)-1)].id
		switch {
		case m.view == 0 && view > 0:
			for _, item := range m.instances {
				if item.socket == id {
					m.bindProject(item)
					break
				}
			}
		case m.view == 1 && view >= 2:
			if m.channel != id {
				m.channel = id
				m.connected = false
				m.device = ""
				m.detail.View.Devices = nil
				m.detail.View.Points = nil
				m.detail.View.PointBindings = nil
				m.detail.View.Rules = nil
				m.pointKind = ""
				m.detail.View.PointKinds = nil
			}
		case m.view == 2 && view >= 3:
			if m.device != id {
				m.device = id
				m.connected = false
				m.detail.View.Points = nil
				m.detail.View.PointBindings = nil
				m.detail.View.Rules = nil
				m.pointKind = ""
				m.detail.View.PointKinds = nil
			}
		}
	}
	m.view, m.cursor, m.pointOffset = view, 0, 0
	m.filter, m.mode = "", ""
	m.detailOpen, m.help = false, false
	m.follow = view == 5 || view == 6
	if view == 0 {
		for i, r := range m.rows() {
			if r.id == m.socket {
				m.cursor = i
				break
			}
		}
	}
}
func (m *model) enter() {
	rows := m.rows()
	if len(rows) == 0 {
		return
	}
	m.cursor = min(m.cursor, len(rows)-1)
	if m.view < 3 {
		m.selectView(m.view + 1)
		return
	}
	m.openDetail()
}
func (m *model) key(key string) bool {
	if key == "ctrl-c" {
		return true
	}
	if m.operationBusy {
		return false
	}
	if m.deleting != nil {
		m.deletionKey(key)
		return false
	}
	if m.picker != nil {
		m.pickerKey(strings.ToLower(key))
		return false
	}
	if m.configurationEditor != nil {
		m.configurationKey(key)
		return false
	}
	if m.editor != nil {
		m.editKey(key)
		return false
	}
	if m.mode == "" {
		key = strings.ToLower(key)
	}
	if m.mode == "" && !m.help && !m.detailOpen {
		if key == "n" {
			m.beginConfiguration("add")
			return false
		}
		if key == "e" {
			m.beginConfiguration("edit")
			return false
		}
		if key == "x" || key == "delete" {
			m.beginDelete()
			return false
		}
		if key == "r" && m.view == 4 {
			m.beginRuleControl()
			return false
		}
		if key == "c" && m.view == 1 {
			m.toggleChannel()
			return false
		}
	}
	if m.mode == "" && !m.help && m.view == 3 {
		if key == "v" {
			m.beginPointEdit()
			return false
		}
		if key == "s" {
			m.reportPoint()
			return false
		}
	}
	if m.detailOpen && m.mode == "" {
		switch key {
		case "esc", "enter":
			m.detailOpen = false
		case "down":
			m.detailScroll++
		case "up":
			m.detailScroll = max(0, m.detailScroll-1)
		case "pagedown":
			m.detailScroll += 10
		case "pageup":
			m.detailScroll = max(0, m.detailScroll-10)
		case "home":
			m.detailScroll = 0
		case "end":
			m.detailScroll = len(m.detailLines)
		case "q":
			return true
		}
		return false
	}
	if m.help {
		switch key {
		case "esc", "?", "f1":
			m.help = false
		case "down":
			m.detailScroll++
		case "up":
			m.detailScroll = max(0, m.detailScroll-1)
		case "pagedown":
			m.detailScroll += 10
		case "pageup":
			m.detailScroll = max(0, m.detailScroll-10)
		case "home":
			m.detailScroll = 0
		case "end":
			m.detailScroll = 1 << 20
		case "q":
			return true
		}
		return false
	}
	if m.mode != "" {
		switch key {
		case "esc":
			m.mode = ""
			m.input = ""
		case "enter":
			if m.mode == "/" {
				m.filter = m.input
				m.cursor = 0
				m.pointOffset = 0
			} else {
				command := strings.ToLower(strings.TrimSpace(m.input))
				if command == "q" || command == "quit" || command == "退出" {
					return true
				}
				found := false
				for i, name := range viewNames {
					if command == strings.ToLower(name) || commandAlias(command) == i {
						m.selectView(i)
						found = true
						break
					}
				}
				if !found {
					m.notice = "未知视图：" + command
				}
			}
			m.mode = ""
			m.input = ""
		case "backspace":
			runes := []rune(m.input)
			if len(runes) > 0 {
				m.input = string(runes[:len(runes)-1])
			}
		case "ctrl-u":
			m.input = ""
		default:
			r, _ := utf8.DecodeRuneInString(key)
			if len(m.input)+len(key) <= 256 && utf8.RuneCountInString(key) == 1 && !unicode.IsControl(r) {
				m.input += key
			}
		}
		return false
	}
	switch key {
	case "q":
		return true
	case "?", "f1":
		m.help = !m.help
		m.detailScroll = 0
	case "/", ":":
		m.mode = key
		m.input = ""
	case "down":
		m.follow = false
		m.cursor++
	case "up":
		m.follow = false
		m.cursor = max(0, m.cursor-1)
	case "home":
		m.follow = false
		m.cursor = 0
	case "end", "g":
		m.follow = m.view == 5 || m.view == 6
		m.cursor = max(0, len(m.rows())-1)
	case "i":
		if m.view < 3 {
			m.openDetail()
		}
	case "f":
		m.selectView(5)
	case "l":
		m.selectView(6)
	case "enter":
		m.enter()
	case "shift-tab":
		m.selectView((m.view + len(viewNames) - 1) % len(viewNames))
	case "tab":
		m.selectView((m.view + 1) % len(viewNames))
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.pointOffset = 0
		} else {
			switch m.view {
			case 0: // The project overview is the root of the navigation tree.
			case 1:
				m.selectView(0)
			case 2:
				m.selectView(1)
			default:
				m.selectView(2)
			}
		}
	case "left":
		m.switchPointKind(-1)
	case "right":
		m.switchPointKind(1)
	case "pageup":
		m.follow = false
		if m.view == 3 {
			m.pointOffset = max(0, m.pointOffset-100)
			m.cursor = 0
		} else {
			m.cursor = max(0, m.cursor-15)
		}
	case "pagedown":
		m.follow = false
		if m.view == 3 {
			if m.pointOffset+100 < m.detail.View.PointMatches {
				m.pointOffset += 100
				m.cursor = 0
			}
		} else {
			m.cursor += 15
		}
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= '7' {
			m.selectView(int(key[0] - '1'))
		}
	}
	return false
}
func fetch(ctx context.Context, req request) update {
	sockets, err := observe.Discover(req.dir)
	result := update{socket: req.socket, request: req, err: err}
	if err != nil {
		return result
	}
	result.instances = make([]instance, len(sockets))
	slots := make(chan struct{}, 8)
	var group sync.WaitGroup
	for i, socket := range sockets {
		group.Add(1)
		go func(i int, socket string) {
			defer group.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			info, err := observe.Fetch(ctx, socket, "/v1/info")
			result.instances[i] = instance{socket, info, err}
		}(i, socket)
	}
	group.Wait()
	sort.Slice(result.instances, func(i, j int) bool { return result.instances[i].socket < result.instances[j].socket })
	// Resolve the whole scope in this request, including direct numeric/command jumps.
	selected := req.socket
	if selected == "" {
		for _, item := range result.instances {
			if item.err == nil {
				selected = item.socket
				break
			}
		}
	}
	result.socket = selected
	if selected != "" {
		var info observe.Info
		for _, item := range result.instances {
			if item.socket == selected {
				info = item.info
				break
			}
		}
		channel := req.channel
		found := false
		for _, c := range info.View.Channels {
			if c.ID == channel {
				found = true
				break
			}
		}
		if !found && len(info.View.Channels) > 0 {
			channel = info.View.Channels[0].ID
		}
		device := req.device
		query := url.Values{"channel": {channel}, "device": {device}, "filter": {req.filter}, "offset": {fmt.Sprint(req.offset)}, "kind": {req.kind}, "order": {"address"}, "after": {fmt.Sprint(req.after)}, "logAfter": {fmt.Sprint(req.logAfter)}}
		result.detail, err = observe.Fetch(ctx, selected, "/v1/view?"+query.Encode())
		if err == nil {
			found = false
			for _, d := range result.detail.View.Devices {
				if d.ID == device {
					found = true
					break
				}
			}
			if !found && len(result.detail.View.Devices) > 0 {
				device = result.detail.View.Devices[0].ID
				query.Set("device", device)
				result.detail, err = observe.Fetch(ctx, selected, "/v1/view?"+query.Encode())
			} else if !found {
				device = ""
			}
		}
		if err == nil {
			result.detail, result.kind, err = pointPage(ctx, selected, query, result.detail)
		}
		result.channel, result.device, result.err = channel, device, err
	}

	return result
}

// Older running engines omit transport metadata. Read their bounded channel
// configuration once per revision, without restarting communication or guessing
// TCP direction from the independent station role.
type channelMetadataCache struct {
	socket, revision string
	started          time.Time
	channels         map[string]core.Channel
}

func (cache *channelMetadataCache) enrich(ctx context.Context, u *update) {
	if u.err != nil || u.socket == "" {
		return
	}
	info := &u.detail
	if cache.socket != u.socket || cache.revision != info.ConfigurationRevision || !cache.started.Equal(info.Started) {
		*cache = channelMetadataCache{socket: u.socket, revision: info.ConfigurationRevision, started: info.Started, channels: make(map[string]core.Channel)}
	}
	for i := range info.View.Channels {
		c := &info.View.Channels[i]
		if c.Transport != "" || !info.ConfigurationOperations {
			continue
		}
		stored, ok := cache.channels[c.ID]
		if !ok {
			view, _, err := observe.Configure(ctx, u.socket, projectfile.Request{Resource: "channel", Action: "read", Channel: c.ID})
			if err != nil || view.Revision != info.ConfigurationRevision {
				continue
			}
			if json.Unmarshal(view.Value["transport"], &stored.Transport) != nil {
				continue
			}
			_ = json.Unmarshal(view.Value["localAddress"], &stored.LocalAddress)
			cache.channels[c.ID] = stored
		}
		c.Transport, c.LocalAddress = stored.Transport, stored.LocalAddress
	}
	for i := range u.instances {
		if u.instances[i].socket == u.socket {
			u.instances[i].info.View.Channels = info.View.Channels
		}
	}
}
func (m *model) apply(u update) {
	oldID := ""
	if rows := m.rows(); len(rows) > 0 {
		oldID = rows[min(m.cursor, len(rows)-1)].id
	}
	m.discovered = true
	// A slow response must not replace a newly chosen project, channel or filter.
	if u.request.socket != m.socket || u.request.channel != m.channel || u.request.device != m.device || u.request.offset != m.pointOffset || u.request.kind != m.pointKind || (m.view == 3 && u.request.filter != m.filter) {
		return
	}
	m.instances = u.instances
	if u.err != nil {
		m.connected = false
		m.notice = "连接错误：" + connectionMessage(u.err)
		m.refreshDetail()
		return
	}
	m.notice = ""
	if u.socket == "" {
		m.connected = false
		m.refreshDetail()
		return
	}
	if m.socket == "" {
		for _, item := range u.instances {
			if item.socket == u.socket {
				m.bindProject(item)
				break
			}
		}
	}
	m.connected = true
	m.socket, m.channel, m.device = u.socket, u.channel, u.device
	m.detail = u.detail
	if u.kind != "" {
		if m.pointKind != u.kind {
			m.pointOffset = 0
		}
		m.pointKind = u.kind
	}
	if m.pointOffset > 0 && m.pointOffset >= m.detail.View.PointMatches {
		m.pointOffset = max(0, (m.detail.View.PointMatches-1)/100*100)
	}
	m.messages = append(m.messages, u.detail.View.Messages...)
	if len(m.messages) > 1000 {
		m.messages = m.messages[len(m.messages)-1000:]
	}
	m.logs = append(m.logs, u.detail.Logs...)
	if len(m.logs) > 1000 {
		m.logs = m.logs[len(m.logs)-1000:]
	}
	m.after, m.logAfter = u.detail.View.Sequence, u.detail.LogSequence
	rows := m.rows()
	if m.follow {
		m.cursor = max(0, len(rows)-1)
	} else if oldID != "" {
		for i, r := range rows {
			if r.id == oldID {
				m.cursor = i
				break
			}
		}
	}
	if oldID == "" && m.view == 0 {
		for i, r := range rows {
			if r.id == m.socket {
				m.cursor = i
				break
			}
		}
	}
	if len(rows) > 0 {
		m.cursor = min(m.cursor, len(rows)-1)
	} else {
		m.cursor = 0
	}
	m.refreshDetail()
}
func Run(ctx context.Context, in, out *os.File, dir string) (result error) {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("prs tui 需要 Unix 终端")
	}
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return fmt.Errorf("prs tui 需要交互式终端")
	}
	if os.Getenv("TERM") == "dumb" {
		return fmt.Errorf("prs tui 需要支持 ANSI 光标控制的终端")
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(in.Fd()), state)
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer fmt.Fprint(out, "\x1b[0m\x1b[?25h\x1b[?1049l")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	keys := make(chan byte, 64)
	go readKeys(ctx, in, keys)
	operationRequests := make(chan manualOperation, 1)
	operationReplies := make(chan manualReply, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case operation := <-operationRequests:
				result, err := observe.Operate(ctx, operation.socket, operation.request)
				select {
				case operationReplies <- manualReply{operation, result, err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	ruleRequests := make(chan ruleOperation, 1)
	ruleReplies := make(chan ruleReply, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case op := <-ruleRequests:
				result, err := observe.ControlRules(ctx, op.socket, op.request)
				select {
				case ruleReplies <- ruleReply{op, result, err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	requests := make(chan request, 1)
	updates := make(chan update, 1)
	go func() {
		var metadata channelMetadataCache
		var bindingNames bindingNamesCache
		for {
			select {
			case <-ctx.Done():
				return
			case req := <-requests:
				value := fetch(ctx, req)
				metadata.enrich(ctx, &value)
				bindingNames.enrich(ctx, &value)
				select {
				case updates <- value:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	configurationRequests := make(chan configurationOperation, 1)
	configurationReplies := make(chan configurationResponse, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case operation := <-configurationRequests:
				reply := performConfiguration(ctx, operation)
				select {
				case configurationReplies <- reply:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	m := model{runtimeDir: dir}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	pending := false
	var escape, utf string
	var escapedAt time.Time
	queue := func() {
		if pending {
			return
		}
		filter := ""
		if m.view == 3 {
			filter = m.filter
		}
		requests <- request{dir: dir, socket: m.socket, channel: m.channel, device: m.device, filter: filter, kind: m.pointKind, offset: m.pointOffset, after: m.after, logAfter: m.logAfter}
		pending = true
	}
	render := func() error {
		width, height, err := term.GetSize(int(out.Fd()))
		if err != nil {
			return err
		}
		_, err = out.WriteString(m.render(width, height))
		return err
	}
	queue()
	for {
		if err = render(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case reply := <-configurationReplies:
			m.applyConfiguration(reply)
			queue()
		case reply := <-ruleReplies:
			m.applyRule(reply)
			queue()
		case reply := <-operationReplies:
			m.applyManual(reply)
			queue()
		case update := <-updates:
			pending = false
			m.apply(update)
		case <-tick.C:
			if escape != "" && time.Since(escapedAt) > 150*time.Millisecond {
				escape = ""
				m.key("esc")
			}
			queue()
		case b, ok := <-keys:
			if !ok {
				return nil
			}
			key := ""
			// Esc followed quickly by a normal key is two actions, not an
			// unknown escape sequence. Preserve the second key (e.g. Esc+r).
			if escape == "\x1b" && b != '[' && b != 'O' {
				m.key("esc")
				escape = ""
			}
			if escape != "" {
				escape += string(b)
				codes := map[string]string{"\x1b[A": "up", "\x1b[B": "down", "\x1b[C": "right", "\x1b[D": "left", "\x1b[5~": "pageup", "\x1b[6~": "pagedown", "\x1b[3~": "delete", "\x1b[Z": "shift-tab", "\x1b[H": "home", "\x1b[F": "end", "\x1bOH": "home", "\x1bOF": "end", "\x1b[1~": "home", "\x1b[4~": "end", "\x1bOP": "f1", "\x1b[11~": "f1"}
				if value, ok := codes[escape]; ok {
					key = value
					escape = ""
				} else if len(escape) > 5 || (len(escape) == 2 && b != '[' && b != 'O') || (len(escape) > 2 && b >= 64 && b <= 126) {
					escape = ""
				}
			} else {
				switch b {
				case 27:
					escape = "\x1b"
					escapedAt = time.Now()
				case 1:
					key = "ctrl-a"
				case 3:
					key = "ctrl-c"
				case 21:
					key = "ctrl-u"
				case 13, 10:
					key = "enter"
				case 9:
					key = "tab"
				case 127, 8:
					key = "backspace"
				default:
					if b >= 32 {
						// Keep the incoming byte; string(b) would re-encode it as a Latin-1 rune.
						utf += string([]byte{b})
						if utf8.FullRuneInString(utf) {
							if utf8.ValidString(utf) {
								key = utf
							}
							utf = ""
						}
					}
				}
			}
			if key != "" {
				if m.key(key) {
					return nil
				}
				if m.pendingConfiguration != nil {
					configurationRequests <- *m.pendingConfiguration
					m.pendingConfiguration = nil
				}
				if m.pendingRule != nil {
					ruleRequests <- *m.pendingRule
					m.pendingRule = nil
				}
				if m.pendingOperation != nil {
					operationRequests <- *m.pendingOperation
					m.pendingOperation = nil
				}
				queue()
			}
		}
	}
}

func wrap(text string, width int) []string {
	text = sanitize(text)
	var lines []string
	var line strings.Builder
	used := 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		w := g.Width()
		if used+w > width {
			lines = append(lines, line.String())
			line.Reset()
			used = 0
		}
		line.WriteString(g.Str())
		used += w
	}
	lines = append(lines, line.String())
	return lines
}
