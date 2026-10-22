package core

import (
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
)

type Point struct {
	WireType    byte    `json:"wireType,omitempty"`
	Timestamp   string  `json:"timestamp,omitempty"`
	TimeInvalid bool    `json:"timeInvalid,omitempty"`
	DataType    string  `json:"dataType,omitempty"`
	ByteOrder   string  `json:"byteOrder,omitempty"`
	Tab         string  `json:"tab,omitempty"`
	Address     uint32  `json:"address"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"` // yx, yc, coil, discrete, holding, input
	Value       float64 `json:"value"`
	Quality     byte    `json:"quality"`
	Updated     string  `json:"updated,omitempty"`
	Feedback    string  `json:"feedback,omitempty"`
}

type PointKey struct {
	Kind    string `json:"kind"`
	Address uint32 `json:"address"`
}

type Device struct {
	AutoData      *AutoDataConfig      `json:"autoData,omitempty"`
	Configuration *DeviceConfiguration `json:"configuration,omitempty"`
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Unit          byte                 `json:"unit"`
	CommonAddress uint16               `json:"commonAddress"`
	Points        []Point              `json:"points"`
}

type Channel struct {
	TransportDraft map[string]string `json:"transportDraft,omitempty"`
	Devices        []Device          `json:"devices"`
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Protocol       string            `json:"protocol"`  // raw, modbus-tcp, modbus-rtu, iec104
	Role           string            `json:"role"`      // client or server
	Transport      string            `json:"transport"` // tcp-client, tcp-server, udp, serial
	Address        string            `json:"address"`
	LocalAddress   string            `json:"localAddress"`
	Unit           byte              `json:"unit"`
	CommonAddress  uint16            `json:"commonAddress"`
	Baud           int               `json:"baud"`
	Parity         string            `json:"parity"`
	DataBits       int               `json:"dataBits"`
	StopBits       int               `json:"stopBits"`
	TimeoutMS      int               `json:"timeoutMs"`
	Points         []Point           `json:"points"`
}

type Project struct {
	Version  int       `json:"version"`
	Name     string    `json:"name"`
	Channels []Channel `json:"channels"`
}

type ChannelState struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Address   string `json:"address"`
	TX        uint64 `json:"tx"`
	RX        uint64 `json:"rx"`
	Peers     int    `json:"peers"`
	LastError string `json:"lastError"`
}

type Message struct {
	Sequence  uint64 `json:"sequence"`
	Time      string `json:"time"`
	ChannelID string `json:"channelId"`
	Peer      string `json:"peer"`
	Direction string `json:"direction"`
	Hex       string `json:"hex"`
	Summary   string `json:"summary"`
}

type Snapshot struct {
	Project  Project        `json:"project"`
	States   []ChannelState `json:"states"`
	Messages []Message      `json:"messages"`
	Events   []PointEvent   `json:"events"`
	Path     string         `json:"path"`
	Dirty    bool           `json:"dirty"`
}

func (c *Channel) Defaults() {
	if c.TimeoutMS == 0 {
		c.TimeoutMS = 3000
	}
	if c.Baud == 0 {
		c.Baud = 9600
	}
	if c.DataBits == 0 {
		c.DataBits = 8
	}
	if c.StopBits == 0 {
		c.StopBits = 1
	}
	if c.Parity == "" {
		c.Parity = "N"
	}
	if c.CommonAddress == 0 {
		c.CommonAddress = 1
	}
	if c.Devices == nil {
		c.Devices = []Device{}
		if c.Protocol != "raw" {
			c.Devices = append(c.Devices, Device{ID: "device-1", Name: "装置 1", Unit: c.Unit, CommonAddress: c.CommonAddress, Points: append([]Point{}, c.Points...)})
		}
	}
	for i := range c.Devices {
		if c.Devices[i].Points == nil {
			c.Devices[i].Points = []Point{}
		}
	}
	c.syncLegacy()
	if c.Points == nil {
		c.Points = []Point{}
	}
}

func (c Channel) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("通道名称不能为空")
	}
	if c.Role != "client" && c.Role != "server" {
		return errors.New("请选择主站或子站")
	}
	if c.Protocol != "raw" && c.Protocol != "modbus-tcp" && c.Protocol != "modbus-rtu" && c.Protocol != "iec104" {
		return errors.New("不支持的协议")
	}
	if c.Protocol == "modbus-tcp" || c.Protocol == "iec104" {
		if c.Transport != "tcp-client" && c.Transport != "tcp-server" {
			return errors.New("此协议需要 TCP 通道")
		}
	}
	if c.Protocol == "modbus-rtu" && c.Transport != "serial" && c.Transport != "tcp-client" && c.Transport != "tcp-server" {
		return errors.New("Modbus RTU 需要串口或 TCP 字节流通道")
	}
	switch c.Transport {
	case "tcp-client", "tcp-server", "udp":
		if err := checkAddress(c.Address, c.Transport == "tcp-server"); err != nil {
			return err
		}
		if c.LocalAddress != "" && c.Transport != "tcp-server" {
			if err := checkAddress(c.LocalAddress, true); err != nil {
				return err
			}
		}
	case "serial":
		if c.Address == "" {
			return errors.New("串口名称不能为空")
		}
		if c.Baud < 50 || c.Baud > 4000000 || c.DataBits < 5 || c.DataBits > 8 || (c.StopBits != 1 && c.StopBits != 2) || !strings.Contains("NEO", c.Parity) || len(c.Parity) != 1 {
			return errors.New("串口参数无效")
		}
	default:
		return errors.New("不支持的传输类型")
	}
	if c.TimeoutMS < 100 || c.TimeoutMS > 60000 {
		return errors.New("超时应为 100–60000 毫秒")
	}
	if len(c.Devices) > 256 {
		return errors.New("每通道最多 256 个装置")
	}
	if c.Protocol == "iec104" && len(c.Devices) > 1 {
		return errors.New("按原版规则，IEC104 每通道仅保留一个装置")
	}
	if c.Protocol == "raw" && len(c.Devices) > 0 {
		return errors.New("RAW 通道不包含规约装置")
	}
	ids := map[string]bool{}
	addresses := map[uint16]bool{}
	for _, d := range c.Devices {
		if err := validateConfiguration(c.Protocol, d.Configuration); err != nil {
			return err
		}
		if d.ID == "" || ids[d.ID] {
			return errors.New("装置 ID 为空或重复")
		}
		ids[d.ID] = true
		if strings.TrimSpace(d.Name) == "" || len([]rune(d.Name)) > 100 {
			return errors.New("装置名称应为 1–100 字")
		}
		address := uint16(d.Unit)
		if c.Protocol == "iec104" {
			address = d.CommonAddress
			if address == 0 || address == 65535 {
				return errors.New("104 公共地址应为 1–65534")
			}
		}
		if c.Protocol == "modbus-rtu" && (d.Unit > 247 || (c.Role == "server" && d.Unit == 0)) {
			return errors.New("RTU 子站地址为 1–247，主站可用 0 广播写入")
		}
		if addresses[address] {
			return errors.New("同一通道装置地址不能重复")
		}
		addresses[address] = true
		if c.Protocol == "iec104" {
			_, caBytes, ioaBytes := layout104(d)
			if uint32(d.CommonAddress) >= 1<<uint(caBytes*8) {
				return errors.New("公共地址超出配置字节数")
			}
			for _, point := range d.Points {
				if point.Address >= 1<<uint(ioaBytes*8) {
					return errors.New("信息体地址超出配置字节数")
				}
			}
		}
		if err := validatePoints(d.Points, c.Protocol); err != nil {
			return fmt.Errorf("%s：%w", d.Name, err)
		}
		if err := validateAutoData(c.Protocol, c.Role, d.AutoData, d.Points...); err != nil {
			return err
		}
	}
	return nil
}

func checkAddress(addr string, allowZero bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return fmt.Errorf("地址应为 主机:端口，例如 127.0.0.1:2404")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 || (n == 0 && !allowZero) {
		return errors.New("端口无效")
	}
	return nil
}

func validatePoints(points []Point, protocol string) error {
	if len(points) > 10000 {
		return errors.New("每个装置最多 10000 个点")
	}
	seen := map[string]bool{}
	for _, p := range points {
		if p.Feedback != "" {
			if _, _, err := parseFeedback(p.Feedback); err != nil {
				return err
			}
		}
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			return errors.New("点值必须是有限数值")
		}
		if p.WireType != 0 && protocol == "iec104" {
			if _, err := pointsASDU([]Point{p}, 1, 3, false); err != nil && (p.Kind == "yx" || p.Kind == "yc" || p.Kind == "mc" || p.Kind == "position") {
				return err
			}
		}
		if p.Address > 0xffffff {
			return errors.New("信息体地址超出 24 位范围")
		}
		if protocol == "iec104" && p.Kind != "yx" && p.Kind != "yc" && p.Kind != "yk" && p.Kind != "yt" && p.Kind != "step" && p.Kind != "mc" && p.Kind != "position" {
			return errors.New("104 点类型无效")
		}
		if strings.HasPrefix(protocol, "modbus") {
			if p.Kind != "coil" && p.Kind != "discrete" && p.Kind != "holding" && p.Kind != "input" {
				return errors.New("Modbus 点类型无效")
			}
			if p.Address > 65535 {
				return errors.New("Modbus 地址超出 16 位范围")
			}
			if p.Kind == "coil" || p.Kind == "discrete" {
				if p.Value != 0 && p.Value != 1 {
					return errors.New("位点值只能是 0 或 1")
				}
			} else if err := validateRegisterPoint(p); err != nil {
				return err
			}
		}
		if (p.Kind == "yx" || p.Kind == "yk") && p.WireType != 3 && p.WireType != 31 && p.Value != 0 && p.Value != 1 {
			return errors.New("单点遥信值只能是 0 或 1")
		}
		if protocol == "iec104" && (p.Kind == "yc" || p.Kind == "yt" || p.Kind == "step") && math.Abs(p.Value) > math.MaxFloat32 {
			return errors.New("遥测值超出短浮点范围")
		}
		if protocol == "iec104" && p.Kind == "step" && (math.Trunc(p.Value) != p.Value || p.Value < -32768 || p.Value > 32767) {
			return errors.New("步调节位置应为 -32768–32767 整数")
		}
		key := fmt.Sprintf("%s:%d", p.Kind, p.Address)
		if seen[key] {
			return fmt.Errorf("点表地址重复：%s", key)
		}
		seen[key] = true
		if strings.HasPrefix(protocol, "modbus") && (p.Kind == "holding" || p.Kind == "input") && registerWidth(p) == 2 {
			second := fmt.Sprintf("%s:%d", p.Kind, p.Address+1)
			if seen[second] {
				return errors.New("多寄存器点位地址重叠")
			}
			seen[second] = true
		}
	}
	return nil
}

func validateProject(p *Project) error {
	if p.Version != 1 && p.Version != 2 {
		return errors.New("不支持的工程格式版本")
	}
	if len(p.Channels) > 64 {
		return errors.New("最多支持 64 个通道")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("工程名称不能为空")
	}
	ids := map[string]bool{}
	for i := range p.Channels {
		c := &p.Channels[i]
		if p.Version == 2 && c.Devices == nil {
			return errors.New("新版工程缺少装置列表")
		}
		c.Defaults()
		if c.ID == "" || ids[c.ID] {
			return errors.New("通道 ID 为空或重复")
		}
		ids[c.ID] = true
		if err := c.Validate(); err != nil {
			return fmt.Errorf("%s：%w", c.Name, err)
		}
	}
	return nil
}

func (c *Channel) syncLegacy() {
	if len(c.Devices) > 0 {
		d := c.Devices[0]
		c.Unit = d.Unit
		c.CommonAddress = d.CommonAddress
		c.Points = d.Points
	} else {
		c.Points = []Point{}
	}
}
