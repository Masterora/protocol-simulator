package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

func bindingPointID(point core.PointKey) string {
	return fmt.Sprintf("%s:%d", point.Kind, point.Address)
}

func bindingText(mappings []core.ControlMapping, names map[string]string) string {
	if len(mappings) == 0 {
		return "未绑定"
	}
	values := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		value := fmt.Sprint(mapping.Target.Address)
		if name := strings.TrimSpace(names[bindingPointID(mapping.Target)]); name != "" {
			value += " " + name
		}
		values = append(values, value)
	}
	return strings.Join(values, "、")
}

type bindingNamesCache struct {
	socket, channel, device, revision string
	started, retryAt                  time.Time
	names                             map[string]string
}

// Running engines that predate binding target names already expose the
// target catalog through the read-only binding editor request.
func (c *bindingNamesCache) enrich(ctx context.Context, u *update) {
	info := &u.detail
	if u.err != nil || len(info.View.PointBindings) == 0 || info.View.BindingTargetNames != nil || !info.ConfigurationOperations || info.ConfigurationRevision == "" {
		return
	}
	if c.socket != u.socket || c.channel != u.channel || c.device != u.device || c.revision != info.ConfigurationRevision || !c.started.Equal(info.Started) {
		*c = bindingNamesCache{socket: u.socket, channel: u.channel, device: u.device, revision: info.ConfigurationRevision, started: info.Started, names: make(map[string]string)}
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	names := make(map[string]string)
	for _, mappings := range info.View.PointBindings {
		for _, mapping := range mappings {
			key := bindingPointID(mapping.Target)
			name, found := c.names[key]
			if !found && time.Now().After(c.retryAt) {
				view, _, err := observe.Configure(ctx, u.socket, projectfile.Request{
					Action: "read", Resource: "binding", Channel: u.channel, Device: u.device, Point: &mapping.Source,
				})
				if err != nil || view.Revision != c.revision {
					c.retryAt = time.Now().Add(2 * time.Second)
				} else {
					for _, p := range view.BindingTargets {
						c.names[bindingPointID(core.PointKey{Kind: p.Kind, Address: p.Address})] = p.Name
					}
					name = c.names[key]
					c.names[key] = name
				}
			}
			names[key] = name
		}
	}
	info.View.BindingTargetNames = names
}

type bindingInput struct {
	kind   string
	invert bool
}

func bindingSelection(addresses []uint32) string {
	sort.Slice(addresses, func(i, j int) bool { return addresses[i] < addresses[j] })
	parts := []string{}
	for i := 0; i < len(addresses); {
		first, last := addresses[i], addresses[i]
		i++
		for i < len(addresses) && uint64(addresses[i]) <= uint64(last)+1 {
			last = addresses[i]
			i++
		}
		part := fmt.Sprint(first)
		if last != first {
			part += "-" + fmt.Sprint(last)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ",")
}

func appendBindingFields(editor *configurationEditor, view projectfile.View) {
	var mappings []core.ControlMapping
	json.Unmarshal(view.Value["mappings"], &mappings)
	var category string
	json.Unmarshal(view.Value["category"], &category)
	targetLabel, targetKind := "遥测", "yc"
	if category == "yk" {
		targetLabel, targetKind = "遥信", "yx"
	}
	editor.bindingStart = len(editor.fields)
	editor.bindingTargets = append([]core.Point{}, view.BindingTargets...)
	editor.bindingLimit = 0xffffff
	if strings.HasPrefix(view.Protocol, "modbus") {
		editor.bindingLimit = 65535
		targetKind = "holding"
		if category == "yk" {
			targetKind = "discrete"
		}
	}
	kinds := []string{}
	for _, target := range editor.bindingTargets {
		if !containsChoice(kinds, target.Kind) {
			kinds = append(kinds, target.Kind)
		}
	}
	if len(kinds) == 0 {
		kinds = append(kinds, targetKind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		directions := []bool{false}
		if category == "yk" {
			directions = append(directions, true)
		}
		for _, invert := range directions {
			label := "绑定" + targetLabel + "点位"
			if invert {
				label = "反向" + targetLabel + "点位"
			}
			if strings.HasPrefix(view.Protocol, "modbus") {
				label += "（" + translated(kind, kindLabels) + "）"
			}
			editor.fields = append(editor.fields, configurationField{key: fmt.Sprintf("binding:%s:%t", kind, invert), label: label})
			editor.bindingInputs = append(editor.bindingInputs, bindingInput{kind, invert})
			addresses := []uint32{}
			for _, mapping := range mappings {
				if mapping.Target.Kind == kind && mapping.Invert == invert {
					addresses = append(addresses, mapping.Target.Address)
				}
			}
			value := bindingSelection(addresses)
			editor.values = append(editor.values, value)
			editor.original = append(editor.original, value)
			editor.replace = append(editor.replace, true)
		}
	}
}

func (e *configurationEditor) bindingFields() (map[string]json.RawMessage, error) {
	mappings := []core.ControlMapping{}
	valid := map[core.PointKey]bool{}
	for _, target := range e.bindingTargets {
		valid[core.PointKey{Kind: target.Kind, Address: target.Address}] = true
	}
	seen := map[core.PointKey]bool{}
	for i, input := range e.bindingInputs {
		index := e.bindingStart + i
		text := strings.TrimSpace(e.values[index])
		if text == "" {
			continue
		}
		addresses, err := core.ParsePointSelection(text, e.bindingLimit)
		if err != nil {
			e.focus = index
			return nil, fmt.Errorf("%s：%w", e.fields[index].label, err)
		}
		for _, address := range addresses {
			target := core.PointKey{Kind: input.kind, Address: address}
			if !valid[target] {
				e.focus = index
				return nil, fmt.Errorf("%s：点位 %d 不存在于当前装置", e.fields[index].label, address)
			}
			if seen[target] {
				e.focus = index
				return nil, fmt.Errorf("点位 %d 不能同时正向和反向绑定", address)
			}
			seen[target] = true
			mappings = append(mappings, core.ControlMapping{Source: *e.operation.request.Point, Target: target, Invert: input.invert})
		}
	}
	return projectfile.Fields(map[string]any{"mappings": mappings}), nil
}
