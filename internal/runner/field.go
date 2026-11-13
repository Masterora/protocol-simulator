package runner

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

type inspectedProject struct {
	Info     observe.Info                    `json:"info"`
	Devices  map[string][]core.InspectDevice `json:"devices,omitempty"`
	Warnings []string                        `json:"warnings,omitempty"`
	Error    string                          `json:"error,omitempty"`
}

func inspectCommand(ctx context.Context, args []string, out, stderr io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	dir := fs.String("runtime-dir", observe.RuntimeDir(), "工程观测`目录`")
	if err := parseFlags(fs, args, out, stderr, "prs inspect [--runtime-dir DIR]"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "inspect 无需位置参数")
		return 2
	}
	sockets, err := observe.Discover(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	projects := make([]inspectedProject, 0, len(sockets))
	for _, socket := range sockets {
		const noFrames = "after=18446744073709551615"
		info, err := observe.Fetch(ctx, socket, "/v1/view?"+noFrames)
		entry := inspectedProject{Info: info}
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.Devices = make(map[string][]core.InspectDevice)
			for _, channel := range info.View.Channels {
				view, err := observe.Fetch(ctx, socket, "/v1/view?"+noFrames+"&channel="+url.QueryEscape(channel.ID))
				if err != nil {
					entry.Warnings = append(entry.Warnings, fmt.Sprintf("%s：%v", channel.Name, err))
					continue
				}
				entry.Devices[channel.ID] = view.View.Devices
			}
		}
		projects = append(projects, entry)
	}
	if err := json.NewEncoder(out).Encode(projects); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func masterCommand(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) == 0 || len(args) == 1 && isHelp(args[0]) {
		printResourceHelp(out, "master")
		return 0
	}
	action := args[0]
	if !supportsAction("master", action) {
		fmt.Fprintln(stderr, "请运行 prs master --help")
		return 2
	}
	fs := flag.NewFlagSet("master "+action, flag.ContinueOnError)
	pid := fs.Int("pid", 0, "运行工程的`进程号`")
	dir := fs.String("runtime-dir", observe.RuntimeDir(), "工程观测`目录`")
	channel := fs.String("channel", "", "通道`标识`")
	device := fs.String("device", "", "装置`标识`")
	req := observe.MasterRequest{Action: action}
	syntax := "prs master " + action + " --pid PID --channel ID --device ID"
	var function *uint
	var selection, value *string
	if action != "interrogate" {
		function = fs.Uint("function", 0, "Modbus `功能码`，十进制")
		selection = fs.String("points", "", "`点位范围`，例如 1,3-5")
		syntax += " --function N --points LIST"
	}
	if action == "write" {
		value = fs.String("value", "", "写入`数值`，0–65535")
		syntax += " --value N"
	}
	if err := parseFlags(fs, args[1:], out, stderr, syntax); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || *pid <= 0 || *channel == "" || *device == "" {
		fmt.Fprintln(stderr, "必须指定工程 PID、通道和装置标识")
		return 2
	}
	req.Channel, req.Device = *channel, *device
	if function != nil {
		if *selection == "" || action == "read" && (*function < 1 || *function > 4) || action == "write" && (*function != 5 && *function != 6 && *function != 15 && *function != 16) {
			fmt.Fprintln(stderr, "请指定点位及有效功能码")
			return 2
		}
		req.Function, req.Selection = byte(*function), *selection
	}
	if value != nil {
		n, err := strconv.ParseUint(*value, 10, 16)
		if err != nil || ((req.Function == 5 || req.Function == 15) && n > 1) {
			fmt.Fprintln(stderr, "寄存器值为 0–65535，线圈值为 0 或 1")
			return 2
		}
		v := uint16(n)
		req.Value = &v
	}
	socket, _, err := findProjectSocket(ctx, *dir, *pid)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := observe.OperateMaster(ctx, socket, req)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if action == "interrogate" {
		fmt.Fprintln(out, "总召唤已发送")
	} else {
		fmt.Fprintf(out, "已提交 %d 个请求，涉及 %d 个点位\n", result.Requests, result.Points)
	}
	return 0
}
