package runner

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func pointCommand(ctx context.Context, args []string, out, stderr io.Writer) int {
	fail := func(code int, err error) int { fmt.Fprintln(stderr, err); return code }
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Fprintln(out, "prs point set|report --pid PID --channel ID --device ID --kind TYPE --address N [--value N] [--quality N] [--runtime-dir DIR] [--json]")
		return 0
	}
	if len(args) == 0 || args[0] != "set" && args[0] != "report" {
		return fail(2, errors.New("usage: prs point set|report --pid PID --channel ID --device ID --kind TYPE --address N [--value N] [--quality N] [--json]"))
	}
	fs := flag.NewFlagSet("point "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	pid := fs.Int("pid", 0, "running project PID")
	dir := fs.String("runtime-dir", observe.RuntimeDir(), "project socket directory")
	channel := fs.String("channel", "", "channel ID")
	device := fs.String("device", "", "device ID")
	kind := fs.String("kind", "", "point kind")
	address := fs.String("address", "", "decimal or hexadecimal address")
	value := fs.String("value", "", "new runtime value")
	quality := fs.String("quality", "", "quality byte (0..255)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || *pid <= 0 || *channel == "" || *device == "" || *kind == "" || *address == "" {
		return fail(2, errors.New("必须明确指定工程 PID、通道、装置、类型和点地址"))
	}
	parseUint := func(text string, bits int) (uint64, error) {
		base := 10
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			base = 0
		}
		return strconv.ParseUint(text, base, bits)
	}
	n, err := parseUint(*address, 32)
	if err != nil {
		return fail(2, fmt.Errorf("点地址无效：%w", err))
	}
	req := core.ManualPointRequest{Channel: *channel, Device: *device, Kind: *kind, Address: uint32(n), Action: args[0]}
	if *value != "" {
		v, e := strconv.ParseFloat(*value, 64)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return fail(2, errors.New("数值必须是有限数字"))
		}
		req.Value = &v
	}
	if *quality != "" {
		q, e := parseUint(*quality, 8)
		if e != nil {
			return fail(2, errors.New("品质应为 0–255 整数"))
		}
		b := byte(q)
		req.Quality = &b
	}
	valueProvided, qualityProvided := false, false
	fs.Visit(func(f *flag.Flag) {
		valueProvided = valueProvided || f.Name == "value"
		qualityProvided = qualityProvided || f.Name == "quality"
	})
	if req.Action == "report" && (valueProvided || qualityProvided) {
		return fail(2, errors.New("report 不接受 --value 或 --quality"))
	}
	if req.Action == "set" && req.Value == nil && req.Quality == nil || req.Action == "report" && (req.Value != nil || req.Quality != nil) {
		return fail(2, errors.New("set 须提供 --value 或 --quality；report 不接受这两个参数"))
	}
	sockets, err := observe.Discover(*dir)
	if err != nil {
		return fail(1, err)
	}
	var selected []string
	for _, socket := range sockets {
		if strings.HasPrefix(filepath.Base(socket), strconv.Itoa(*pid)+"-") {
			selected = append(selected, socket)
		}
	}
	if len(selected) != 1 {
		return fail(1, errors.New("未找到唯一的目标工程，请核对 PID、运行用户和观测目录"))
	}
	info, err := observe.Fetch(ctx, selected[0], "/v1/info")
	if err != nil {
		return fail(1, err)
	}
	if info.PID != *pid || !info.PointOperations {
		return fail(1, errors.New("目标进程不支持手动点位操作，请升级该工程的服务进程"))
	}
	req.Metadata = info.View.Metadata
	result, err := observe.Operate(ctx, selected[0], req)
	if *asJSON {
		if encodeErr := json.NewEncoder(out).Encode(result); encodeErr != nil {
			return fail(1, encodeErr)
		}
	} else {
		if result.Point != nil {
			fmt.Fprintf(out, "%s:%d = %g · 品质 %d\n", result.Point.Kind, result.Point.Address, result.Point.Value, result.Point.Quality)
		}
		switch result.Mode {
		case "reported":
			fmt.Fprintf(out, "已上报 %d 个连接\n", result.Receivers)
		case "offline":
			if result.Applied {
				fmt.Fprintln(out, "已修改运行值；连接未激活，未上报")
			}
		case "polled":
			fmt.Fprintln(out, "已修改运行值，主站下次读取生效")
		}
	}
	if err != nil {
		return fail(1, err)
	}
	return 0
}
