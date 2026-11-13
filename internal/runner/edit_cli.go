package runner

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"gytest-go/internal/core"
	"gytest-go/internal/instance"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

type assignments []string

func (a *assignments) String() string     { return strings.Join(*a, ",") }
func (a *assignments) Set(s string) error { *a = append(*a, s); return nil }
func findProjectSocket(ctx context.Context, dir string, pid int) (string, observe.Info, error) {
	sockets, err := observe.Discover(dir)
	if err != nil {
		return "", observe.Info{}, err
	}
	var matches []string
	for _, socket := range sockets {
		if strings.HasPrefix(filepath.Base(socket), strconv.Itoa(pid)+"-") {
			matches = append(matches, socket)
		}
	}
	if len(matches) != 1 {
		return "", observe.Info{}, errors.New("未找到唯一工程进程，请核对 PID、运行用户和观测目录")
	}
	info, err := observe.Fetch(ctx, matches[0], "/v1/info")
	if err != nil {
		return "", info, err
	}
	if info.PID != pid {
		return "", info, errors.New("工程 PID 不匹配")
	}
	return matches[0], info, nil
}
func configurationCommand(ctx context.Context, resource string, args []string, out, stderr io.Writer) int {
	fail := func(code int, err error) int { fmt.Fprintln(stderr, err); return code }
	if len(args) == 0 || isHelp(args[0]) {
		printResourceHelp(out, resource)
		return 0
	}
	action := args[0]
	args = args[1:]
	if !supportsAction(resource, action) {
		return fail(2, fmt.Errorf("不支持操作 %q，请运行 prs %s --help 查看用法", action, resource))
	}
	var positional string
	if resource == "project" && action == "new" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet(resource+" "+action, flag.ContinueOnError)
	pid := fs.Int("pid", 0, "运行中的工程`进程号`")
	file := fs.String("file", positional, "工程 JSON `文件`路径，用于离线编辑")
	dir := fs.String("runtime-dir", observe.RuntimeDir(), "工程观测套接字所在`目录`")
	channel := fs.String("channel", "", "通道`标识`")
	device := fs.String("device", "", "装置`标识`")
	id := fs.String("id", "", "通道或装置`标识`，作为 --channel 或 --device 的别名")
	kind := fs.String("kind", "", "点位`类型`")
	address := fs.String("address", "", "十进制或十六进制`点位`")
	index := fs.Int("index", 0, "规则`序号`，从 1 开始")
	name := fs.String("name", "", "工程`名称`")
	start := fs.Bool("start", false, "保存后启动独立的空工程")
	asJSON := fs.Bool("json", false, "以 JSON 格式输出")
	revision := fs.String("revision", "", "预期配置`修订号`，省略时读取当前修订号")
	var sets assignments
	fs.Var(&sets, "set", "设置`字段=值`，可重复指定")
	syntax, visible := configurationHelp(resource, action)
	if err := parseFlags(fs, args, out, stderr, syntax, visible...); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return fail(2, errors.New("不接受额外位置参数"))
	}
	if *pid < 0 || *index < 0 || (*index != 0 && (resource != "rule" || action != "edit" && action != "delete")) {
		return fail(2, errors.New("PID 不可为负；--index 只用于编辑或删除规则且从 1 开始"))
	}
	if (action == "start" || action == "stop") && (resource != "channel" || len(sets) > 0 || *device != "" || *kind != "" || *address != "") {
		return fail(2, errors.New("start/stop 仅用于连接通道，不接受字段或点位参数"))
	}
	if *id != "" && action == "add" {
		return fail(2, errors.New("新增 ID 请使用 --set id=VALUE"))
	}
	if resource == "project" && action == "new" {
		if *file == "" || *pid != 0 || len(sets) > 0 {
			return fail(2, errors.New("project new 须指定新文件路径，不接受 PID 或 --set"))
		}
		doc, err := projectfile.Create(*file, *name)
		if err != nil {
			return fail(1, err)
		}
		result := projectfile.Result{Saved: true, Path: doc.Path, Revision: doc.Revision, Message: "空工程已保存"}
		payload := map[string]any{"result": result}
		if *start {
			child, err := instance.Start(ctx, doc.Path, *dir)
			payload["instance"] = child
			if err != nil {
				result.Error = err.Error()
				payload["result"] = result
				if *asJSON {
					json.NewEncoder(out).Encode(payload)
				}
				return fail(1, err)
			}
		}
		if *asJSON {
			json.NewEncoder(out).Encode(payload)
		} else {
			fmt.Fprintln(out, result.Message+"："+doc.Path)
		}
		return 0
	}
	if *name != "" || *start {
		return fail(2, errors.New("--name 和 --start 只用于 project new"))
	}
	if (*pid > 0) == (*file != "") {
		return fail(2, errors.New("必须且只能指定 --pid 或 --file"))
	}
	if action == "delete" && len(sets) > 0 {
		return fail(2, errors.New("删除不接受 --set 字段"))
	}
	if action == "delete" && resource == "project" && *pid == 0 {
		return fail(2, errors.New("删除工程须指定 --pid，以确保停止对应运行进程"))
	}
	req := projectfile.Request{Resource: resource, Action: action, Channel: *channel, Device: *device, Fields: map[string]json.RawMessage{}}
	if *id != "" {
		if resource == "channel" {
			req.Channel = *id
		} else if resource == "device" {
			req.Device = *id
		} else {
			return fail(2, errors.New("--id 只用于通道或装置"))
		}
	}
	if *kind != "" || *address != "" {
		if resource != "point" || *kind == "" || *address == "" {
			return fail(2, errors.New("点位须同时指定 --kind 和 --address"))
		}
		base := 10
		if strings.HasPrefix(strings.ToLower(*address), "0x") {
			base = 0
		}
		n, err := strconv.ParseUint(*address, base, 32)
		if err != nil {
			return fail(2, err)
		}
		if action == "add" {
			req.Fields = projectfile.Fields(map[string]any{"kind": *kind, "address": uint32(n)})
		} else {
			req.Point = &core.PointKey{Kind: *kind, Address: uint32(n)}
		}
	}
	if *index > 0 {
		i := *index - 1
		req.Index = &i
	} else if resource == "rule" && (action == "edit" || action == "delete") {
		return fail(2, errors.New("编辑或删除规则须指定从 1 开始的 --index"))
	}
	for _, set := range sets {
		key, value, ok := strings.Cut(set, "=")
		if !ok || key == "" {
			return fail(2, errors.New("--set 格式应为 field=value"))
		}
		if _, exists := req.Fields[key]; exists {
			return fail(2, fmt.Errorf("字段 %s 重复", key))
		}
		raw, err := projectfile.ParseField(resource, key, value)
		if err != nil {
			return fail(2, err)
		}
		req.Fields[key] = raw
	}
	var result projectfile.Result
	if *pid > 0 {
		socket, info, err := findProjectSocket(ctx, *dir, *pid)
		if err != nil {
			return fail(1, err)
		}
		if !info.ConfigurationOperations {
			return fail(1, errors.New("运行中的工程不支持配置编辑，请升级服务进程"))
		}
		req.Revision = info.ConfigurationRevision
		if *revision != "" {
			req.Revision = *revision
		}
		_, result, err = observe.Configure(ctx, socket, req)
		if *asJSON {
			json.NewEncoder(out).Encode(result)
		} else if result.Message != "" {
			fmt.Fprintln(out, result.Message)
		}
		if err != nil {
			return fail(1, err)
		}
		return 0
	}
	if action == "start" || action == "stop" {
		return fail(2, errors.New("通道连接操作须指定运行中的 --pid"))
	}
	doc, err := projectfile.Read(*file)
	if err != nil {
		return fail(1, err)
	}
	if *revision != "" && *revision != doc.Revision {
		return fail(1, projectfile.ErrConflict)
	}
	next, result, err := projectfile.Apply(doc.Project, req)
	if err != nil {
		return fail(1, err)
	}
	newRevision, err := projectfile.Commit(doc.Path, doc.Revision, next)
	if err != nil {
		return fail(1, err)
	}
	result.Saved, result.Path, result.Revision = true, doc.Path, newRevision
	if action == "delete" {
		result.Deleted = true
	}
	result.Message = "配置文件已保存；运行状态未确认（运行中请使用 --pid）"
	if action == "delete" {
		result.Message = "已永久删除配置并保存；运行状态未确认（运行中请使用 --pid）"
	}
	if *asJSON {
		json.NewEncoder(out).Encode(result)
	} else {
		fmt.Fprintln(out, result.Message)
	}
	return 0
}
