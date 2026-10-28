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

const usage = `prs — protocol-simulator server
Usage:
  prs tui [--runtime-dir DIR]
  prs point set|report --pid PID --channel ID --device ID --kind TYPE --address N [--value N] [--quality N] [--json]
  prs version
  prs ports [--json]
  prs validate PROJECT [--json]
  prs run PROJECT [--channels all|ID,ID] [--start-data-rules] [--log-format text|json]
                 [--frames FILE --frames-max-mb 10 --frames-files 3] [--watch=false] [--runtime-dir DIR]

run explicitly starts the selected channels. Automatic control/setpoint jobs are
never started. Existing project files are read-only. Run ./bin/prs from the
extracted package, or prs after adding its bin directory to PATH.
`

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
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, usage)
		return 0
	}
	fail := func(code int, err error) int { fmt.Fprintln(stderr, err); return code }
	switch args[0] {
	case "point":
		return pointCommand(ctx, args[1:], out, stderr)
	case "tui":
		fs := flag.NewFlagSet("tui", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dir := fs.String("runtime-dir", observe.RuntimeDir(), "project socket directory")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() != 0 {
			return fail(2, errors.New("tui connects to running projects; it does not accept a project file"))
		}
		terminal, ok := out.(*os.File)
		if !ok {
			return fail(1, errors.New("prs tui requires terminal output"))
		}
		if err := tui.Run(ctx, os.Stdin, terminal, *dir); err != nil {
			return fail(1, err)
		}
		return 0
	case "version", "--version":
		if len(args) != 1 {
			return fail(2, errors.New("version accepts no arguments"))
		}
		fmt.Fprintf(out, "prs %s\n", version)
		return 0
	case "ports":
		fs := flag.NewFlagSet("ports", flag.ContinueOnError)
		fs.SetOutput(stderr)
		asJSON := fs.Bool("json", false, "JSON output")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() != 0 {
			return fail(2, errors.New("ports accepts no positional arguments"))
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
		fs.SetOutput(stderr)
		opt := options{project: project}
		fs.StringVar(&opt.project, "project", project, "project JSON path")
		asJSON := false
		if args[0] == "validate" {
			fs.BoolVar(&asJSON, "json", false, "JSON output")
		} else {
			fs.StringVar(&opt.runtimeDir, "runtime-dir", observe.RuntimeDir(), "local observation socket directory")
			fs.BoolVar(&opt.watch, "watch", true, "automatically reload validated project changes")
			fs.StringVar(&opt.channels, "channels", "all", "comma-separated channel IDs or all")
			fs.BoolVar(&opt.startData, "start-data-rules", false, "start saved data rules; IEC104 waits for link activation")
			fs.StringVar(&opt.format, "log-format", "text", "text or json")
			fs.StringVar(&opt.frames, "frames", "", "optional rotating JSONL frame log")
			fs.IntVar(&opt.frameMB, "frames-max-mb", 10, "maximum MiB per frame log file")
			fs.IntVar(&opt.frameFiles, "frames-files", 3, "total frame log files (1..10)")
		}
		if err := fs.Parse(rest); err != nil {
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
			return fail(2, errors.New("project path was provided twice"))
		}
		if opt.project == "" && fs.NArg() == 1 {
			opt.project = fs.Arg(0)
		} else if fs.NArg() != 0 {
			return fail(2, errors.New("provide exactly one project path; place flags before a trailing path"))
		}
		if opt.project == "" {
			return fail(2, errors.New("project path is required"))
		}
		absolute, absErr := filepath.Abs(opt.project)
		if absErr != nil {
			return fail(2, absErr)
		}
		opt.project = absolute
		if args[0] == "run" && (opt.format != "text" && opt.format != "json" || opt.frameMB < 1 || opt.frameMB > 1024 || opt.frameFiles < 1 || opt.frameFiles > 10) {
			return fail(2, errors.New("invalid log format or frame limits"))
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
				fmt.Fprintf(out, "Valid: %s (%d channels)\n", projectInfo.Name, len(projectInfo.Channels))
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
		return fail(2, fmt.Errorf("unknown command %q; use prs help", args[0]))
	}
}
func selectChannels(channels []core.Channel, selection string) ([]core.Channel, error) {
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
