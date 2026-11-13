package tui

import (
	"fmt"
	"strconv"
	"strings"
)

type endpointCursor struct {
	part, at int
	active   bool
}

type endpointParts [5]string

func parseEndpoint(value string) (endpointParts, bool) {
	var out endpointParts
	if value == "" {
		return out, true
	}
	host, port, ok := strings.Cut(value, ":")
	if !ok {
		return out, false
	}
	octets := strings.Split(host, ".")
	if len(octets) != 4 {
		return out, false
	}
	copy(out[:4], octets)
	out[4] = port
	for i, part := range out {
		if len(part) > endpointPartWidth(i) {
			return out, false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return out, false
			}
		}
	}
	return out, true
}

func endpointPartWidth(i int) int {
	if i == 4 {
		return 5
	}
	return 3
}

func (p endpointParts) string() string {
	if p == (endpointParts{}) {
		return ""
	}
	return strings.Join(p[:4], ".") + ":" + p[4]
}

func (p endpointParts) checked() (string, error) {
	for i, value := range p {
		bits := 8
		if i == 4 {
			bits = 16
		}
		n, err := strconv.ParseUint(value, 10, bits)
		if err != nil {
			if i == 4 {
				return "", fmt.Errorf("端口应为 0–65535 的整数")
			}
			return "", fmt.Errorf("IPv4 第 %d 段应为 0–255 的整数", i+1)
		}
		p[i] = strconv.FormatUint(n, 10)
	}
	return p.string(), nil
}

func (c endpointCursor) position(p endpointParts) (int, int) {
	if c.active {
		return c.part, min(c.at, len(p[c.part]))
	}
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] != "" {
			return i, len(p[i])
		}
	}
	return 0, 0
}

func (c *endpointCursor) key(parts *endpointParts, replace *bool, key string) bool {
	i, at := c.position(*parts)
	switch key {
	case "ctrl-u":
		*parts, i, at = endpointParts{}, 0, 0
	case "home":
		i, at = 0, 0
	case "end":
		i, at = 4, len(parts[4])
	case "left":
		if at > 0 {
			at--
		} else if i > 0 {
			i--
			at = len(parts[i])
		}
	case "right":
		if at < len(parts[i]) {
			at++
		} else if i < 4 {
			i++
			at = 0
		}
	case ".":
		if i < 3 {
			i++
			at = 0
		}
	case ":":
		i, at = 4, 0
	case "backspace":
		for at == 0 && i > 0 {
			i--
			at = len(parts[i])
		}
		if at > 0 {
			parts[i] = parts[i][:at-1] + parts[i][at:]
			at--
		}
	case "delete":
		for at == len(parts[i]) && i < 4 {
			i++
			at = 0
		}
		if at < len(parts[i]) {
			parts[i] = parts[i][:at] + parts[i][at+1:]
		}
	default:
		if len(key) != 1 || key[0] < '0' || key[0] > '9' {
			return false
		}
		if *replace {
			*parts, i, at = endpointParts{}, 0, 0
		}
		if at == endpointPartWidth(i) && i < 4 {
			i++
			at = 0
		}
		if len(parts[i]) < endpointPartWidth(i) {
			parts[i] = parts[i][:at] + key + parts[i][at:]
			at++
		} else if at < len(parts[i]) {
			parts[i] = parts[i][:at] + key + parts[i][at+1:]
			at++
		}
	}
	*replace = false
	*c = endpointCursor{part: i, at: at, active: true}
	return true
}

func (c endpointCursor) render(parts endpointParts, focused bool) string {
	part, at := c.position(parts)
	var result strings.Builder
	for i, value := range parts {
		width := endpointPartWidth(i)
		if i == 4 && value == "" && !focused {
			width = 0
		}
		if focused && i == part {
			value = fit(value, width)
			result.WriteString(value[:at] + inputCaret + value[at:])
		} else {
			result.WriteString(fit(value, width))
		}
		switch {
		case i < 3:
			result.WriteByte('.')
		case i == 3:
			result.WriteByte(':')
		}
	}
	return result.String()
}
