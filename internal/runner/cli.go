package runner

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"gytest-go/internal/observe"
	"gytest-go/internal/tui"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gytest-go/internal/core"
)

type options struct {
	project, channels, format, frames, runtimeDir string
	startData, watch                              bool
	frameMB, frameFiles                           int
}

func projectArguments(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}
func Execute(ctx context.Context, args []string, out, stderr io.Writer, version string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || len(args) == 1 && args[0] == "help" {
		fmt.Fprint(out, usage)
		return 0
	}
	fail := func(code int, err error) int { fmt.Fprintln(stderr, err); return code }
	if args[0] == "help" {
		if len(args) > 3 || isHelp(args[1]) || len(args) == 3 && !supportsAction(args[1], args[2]) {
			return fail(2, errors.New("帮助用法：prs help <命令> [操作]"))
		}
		args = append(append([]string{}, args[1:]...), "--help")
	}
	switch args[0] {
	case "project", "channel", "device", "rule":
		return configurationCommand(ctx, args[0], args[1:], out, stderr)
	case "point":
		if len(args) == 1 || len(args) == 2 && isHelp(args[1]) {
			printResourceHelp(out, "point")
			return 0
		}
		if len(args) > 1 && (args[1] == "add" || args[1] == "edit" || args[1] == "delete") {
			return configurationCommand(ctx, "point", args[1:], out, stderr)
		}
		return pointCommand(ctx, args[1:], out, stderr)
	case "inspect":
		return inspectCommand(ctx, args[1:], out, stderr)
	case "master":
		return masterCommand(ctx, args[1:], out, stderr)
	case "tui":
		fs := flag.NewFlagSet("tui", flag.ContinueOnError)
		dir := fs.String("runtime-dir", observe.RuntimeDir(), "工程观测套接字所在`目录`")
		if err := parseFlags(fs, args[1:], out, stderr, "prs "+args[0]+" [选项]"); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() != 0 {
			return fail(2, errors.New("tui 用于查看运行中的工程，请直接执行 prs tui"))
		}
		terminal, ok := out.(*os.File)
		if !ok {
			return fail(1, errors.New("prs tui 需要在终端中运行"))
		}
		if err := tui.Run(ctx, os.Stdin, terminal, *dir); err != nil {
			return fail(1, err)
		}
		return 0
	case "version", "--version":
		if len(args) == 2 && args[0] == "version" && isHelp(args[1]) {
			fmt.Fprintln(out, "用法\n  prs version\n\n显示 prs 版本。")
			return 0
		}
		if len(args) != 1 {
			return fail(2, errors.New("version 无需参数"))
		}
		fmt.Fprintf(out, "prs %s\n", version)
		return 0
	case "ports":
		fs := flag.NewFlagSet("ports", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "以 JSON 格式输出")
		if err := parseFlags(fs, args[1:], out, stderr, "prs "+args[0]+" [选项]"); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() != 0 {
			return fail(2, errors.New("ports 无需位置参数"))
		}
		ports, err := core.SerialPorts()
		if err != nil {
			return fail(1, err)
		}
		if ports == nil {
			ports = []string{}
		}
		if *asJSON {
			if err = json.NewEncoder(out).Encode(ports); err != nil {
				return fail(1, err)
			}
		} else {
			for _, port := range ports {
				fmt.Fprintln(out, port)
			}
		}
		return 0
	case "run", "validate":
		project, rest := projectArguments(args[1:])
		fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
		opt := options{project: project}
		fs.StringVar(&opt.project, "project", project, "工程 JSON `文件`路径")
		asJSON := false
		if args[0] == "validate" {
			fs.BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
		} else {
			fs.StringVar(&opt.runtimeDir, "runtime-dir", observe.RuntimeDir(), "工程观测套接字所在`目录`")
			fs.BoolVar(&opt.watch, "watch", true, "校验并自动加载配置变更，可用 --watch=false 关闭")
			fs.StringVar(&opt.channels, "channels", "all", "选择通道，可填 all、none 或逗号分隔的`标识`")
			fs.BoolVar(&opt.startData, "start-data-rules", false, "启动已启用的自动上报规则，IEC104 等待链路激活")
			fs.StringVar(&opt.format, "log-format", "text", "运行日志`格式`，可填 text 或 json")
			fs.StringVar(&opt.frames, "frames", "", "将收发报文写入可轮转的 JSONL `文件`")
			fs.IntVar(&opt.frameMB, "frames-max-mb", 10, "单个报文日志的大小`上限`，单位为 MiB")
			fs.IntVar(&opt.frameFiles, "frames-files", 3, "保留报文日志的文件`数量`，范围为 1–10")
		}
		if err := parseFlags(fs, rest, out, stderr, "prs "+args[0]+" (<工程文件> | --project <工程文件>) [选项]"); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		duplicateProject := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "project" && project != "" {
				duplicateProject = true
			}
		})
		if duplicateProject {
			return fail(2, errors.New("工程文件路径重复，请只指定一次"))
		}
		if opt.project == "" && fs.NArg() == 1 {
			opt.project = fs.Arg(0)
		} else if fs.NArg() != 0 {
			return fail(2, errors.New("请指定一个工程文件，或使用 --project 指定路径"))
		}
		if opt.project == "" {
			return fail(2, errors.New("请指定工程文件路径"))
		}
		absolute, absErr := filepath.Abs(opt.project)
		if absErr != nil {
			return fail(2, absErr)
		}
		opt.project = absolute
		if args[0] == "run" && (opt.format != "text" && opt.format != "json" || opt.frameMB < 1 || opt.frameMB > 1024 || opt.frameFiles < 1 || opt.frameFiles > 10) {
			return fail(2, errors.New("日志格式或报文日志限制无效"))
		}
		projectInfo, err := core.ReadProject(opt.project)
		if err != nil {
			return fail(2, err)
		}
		engine := core.NewEngine()
		if err := engine.ReplaceProject(projectInfo, opt.project); err != nil {
			return fail(2, err)
		}
		if args[0] == "validate" {
			if asJSON {
				if err := json.NewEncoder(out).Encode(map[string]any{"valid": true, "name": projectInfo.Name, "channels": len(projectInfo.Channels)}); err != nil {
					return fail(1, err)
				}
			} else {
				fmt.Fprintf(out, "校验通过：%s（%d 个通道）\n", projectInfo.Name, len(projectInfo.Channels))
			}
			return 0
		}
		selected, err := selectChannels(projectInfo.Channels, opt.channels)
		if err != nil {
			return fail(2, err)
		}
		var frames *rotatingLog
		if opt.frames != "" {
			frames, err = openFrames(opt.frames, opt.project, int64(opt.frameMB)<<20, opt.frameFiles)
			if err != nil {
				return fail(2, err)
			}
		}
		sink := newLogSink(out, opt.format, frames)
		err = run(ctx, engine, projectInfo, selected, opt, sink)
		flushErr := sink.close()
		if err == nil {
			err = flushErr
		}
		if err != nil {
			return fail(1, err)
		}
		return 0
	default:
		return fail(2, fmt.Errorf("未知命令 %q，请运行 prs --help 查看指令集", args[0]))
	}
}
func selectChannels(channels []core.Channel, selection string) ([]core.Channel, error) {
	if selection == "none" {
		return []core.Channel{}, nil
	}
	if selection == "all" {
		return channels, nil
	}
	if len(channels) == 0 {
		return nil, errors.New("project has no channels")
	}
	wanted := map[string]bool{}
	for _, id := range strings.Split(selection, ",") {
		id = strings.TrimSpace(id)
		if id == "" || wanted[id] {
			return nil, errors.New("channel selection contains an empty or duplicate ID")
		}
		wanted[id] = true
	}
	var selected []core.Channel
	for _, c := range channels {
		if wanted[c.ID] {
			selected = append(selected, c)
			delete(wanted, c.ID)
		}
	}
	if len(wanted) > 0 {
		return nil, fmt.Errorf("unknown channel IDs: %v", wanted)
	}
	return selected, nil
}
