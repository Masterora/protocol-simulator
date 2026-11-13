package runner

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

const usage = `prs · protocol-simulator 服务端

用法
  prs <命令> [参数]

命令
  tui        打开终端管理界面
  run        运行工程
  validate   校验工程配置
  inspect    只读查看运行工程（JSON）
  master     主站总召唤与 Modbus 读写
  project    管理工程
  channel    管理通道
  device     管理装置
  point      管理点位、修改点值和立即上报
  rule       管理自动上报规则
  ports      列出串口
  version    显示版本
  help       查看命令帮助

选项
  -h, --help  显示此帮助
  --version   显示版本

示例
  prs tui
  prs validate gateway.json --json
  prs help run
  prs help point report
`

type commandAction struct {
	name, description string
}

var resourceActions = map[string][]commandAction{
	"master":  {{"interrogate", "发送 IEC104 总召唤"}, {"read", "读取 Modbus 点位"}, {"write", "写入 Modbus 点位"}},
	"project": {{"new", "新建工程"}, {"edit", "编辑工程"}, {"delete", "删除工程"}},
	"channel": {{"add", "新增通道"}, {"edit", "编辑通道"}, {"delete", "删除通道"}, {"start", "启动通道"}, {"stop", "停止通道"}},
	"device":  {{"add", "新增装置"}, {"edit", "编辑装置"}, {"delete", "删除装置"}},
	"point":   {{"add", "新增点位"}, {"edit", "编辑点位"}, {"delete", "删除点位"}, {"set", "修改点值或品质"}, {"report", "立即上报 IEC104 点值"}},
	"rule":    {{"add", "新增规则"}, {"edit", "编辑规则"}, {"delete", "删除规则"}},
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func supportsAction(resource, name string) bool {
	for _, action := range resourceActions[resource] {
		if action.name == name {
			return true
		}
	}
	return false
}

func printResourceHelp(out io.Writer, resource string) {
	fmt.Fprintf(out, "用法\n  prs %s <操作> [选项]\n\n操作\n", resource)
	for _, action := range resourceActions[resource] {
		fmt.Fprintf(out, "  %-8s %s\n", action.name, action.description)
	}
	fmt.Fprintf(out, "\n查看操作帮助\n  prs %s <操作> --help\n", resource)
}

// parseFlags keeps successful help on stdout and argument errors on stderr.
func parseFlags(fs *flag.FlagSet, args []string, out, stderr io.Writer, syntax string, visible ...string) error {
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintf(out, "用法\n  %s\n\n选项\n", syntax)
		if len(visible) == 0 {
			fs.VisitAll(func(f *flag.Flag) { printFlag(out, f) })
		} else {
			for _, name := range visible {
				printFlag(out, fs.Lookup(name))
			}
		}
		fmt.Fprintln(out, "  -h, --help\n      显示此帮助")
	} else if err != nil {
		fmt.Fprintf(stderr, "%s\n请运行 prs %s --help 查看选项。\n", err, fs.Name())
	}
	return err
}

func printFlag(out io.Writer, f *flag.Flag) {
	arg, description := flag.UnquoteUsage(f)
	fmt.Fprintf(out, "  --%s", f.Name)
	if arg != "" {
		fmt.Fprintf(out, " <%s>", arg)
	}
	fmt.Fprintf(out, "\n      %s", description)
	if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
		fmt.Fprintf(out, "（默认 %s）", f.DefValue)
	}
	fmt.Fprintln(out)
}

func configurationHelp(resource, action string) (string, []string) {
	syntax := "prs " + resource + " " + action
	common := []string{"runtime-dir", "json"}
	if resource == "project" && action == "new" {
		return syntax + " (<工程文件> | --file <工程文件>) [选项]", append([]string{"file", "name", "start"}, common...)
	}
	flags := []string{"pid"}
	target := " (--pid <进程号> | --file <工程文件>)"
	if action == "start" || action == "stop" || resource == "project" && action == "delete" {
		target = " --pid <进程号>"
	} else {
		flags = append(flags, "file")
	}
	switch resource {
	case "channel":
		if action != "add" {
			target += " --channel <通道标识>"
			flags = append(flags, "channel", "id")
		}
	case "device":
		target += " --channel <通道标识>"
		flags = append(flags, "channel")
		if action != "add" {
			target += " --device <装置标识>"
			flags = append(flags, "device", "id")
		}
	case "point", "rule":
		target += " --channel <通道标识> --device <装置标识>"
		flags = append(flags, "channel", "device")
		if resource == "point" {
			flags = append(flags, "kind", "address")
			if action != "add" {
				target += " --kind <类型> --address <点位>"
			}
		} else if action != "add" {
			target += " --index <序号>"
			flags = append(flags, "index")
		}
	}
	if action == "add" || action == "edit" {
		flags = append(flags, "set")
	}
	flags = append(flags, "revision")
	return syntax + target + " [选项]", append(flags, common...)
}
