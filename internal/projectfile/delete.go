package projectfile

import (
	"errors"
	"gytest-go/internal/core"
	"os"
)

// deleteResource removes one configuration resource from a detached project. Project
// files use Remove so permanent deletion is guarded by the same revision lock.
func deleteResource(p core.Project, req Request) (core.Project, Result, error) {
	result := Result{Channel: req.Channel, Device: req.Device, Point: req.Point, Index: req.Index}
	fail := func(err error) (core.Project, Result, error) { return core.Project{}, result, err }
	if len(req.Fields) != 0 {
		return fail(errors.New("删除不接受配置字段"))
	}
	if req.Resource == "project" {
		return fail(errors.New("删除工程文件须使用运行工程的删除接口"))
	}
	c, err := channel(&p, req.Channel)
	if err != nil {
		return fail(err)
	}
	if req.Resource == "channel" {
		for i, v := range p.Channels {
			if v.ID == req.Channel {
				p.Channels = append(p.Channels[:i], p.Channels[i+1:]...)
				break
			}
		}
	} else {
		d, err := device(c, req.Device)
		if err != nil {
			return fail(err)
		}
		switch req.Resource {
		case "device":
			for i, v := range c.Devices {
				if v.ID == req.Device {
					c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
					break
				}
			}
		case "point":
			target, err := point(d, req.Point)
			if err != nil {
				return fail(err)
			}
			if core.ServerPointReferenced(*d, *req.Point) {
				return fail(errors.New("此点被自动上报规则引用，请先修改或删除相关规则"))
			}
			for i := range d.Points {
				if &d.Points[i] == target {
					d.Points = append(d.Points[:i], d.Points[i+1:]...)
					break
				}
			}
			if err = core.ValidateServerPointReferences(*c, *d); err != nil {
				return fail(err)
			}
		case "rule":
			cfg := RuleConfiguration(*d)
			if req.Index == nil || *req.Index < 0 || *req.Index >= len(cfg.Rules) {
				return fail(errors.New("上报规则已变化，请刷新"))
			}
			cfg.Rules = append(cfg.Rules[:*req.Index], cfg.Rules[*req.Index+1:]...)
			if d.DataTests == nil {
				d.DataTests = map[string]*core.AutoDataConfig{}
			}
			for _, key := range []string{"yx", "yc", "yx-report", "yc-report"} {
				delete(d.DataTests, key)
			}
			d.AutoData = nil
			d.DataTests["telemetry"] = &cfg
		default:
			return fail(errors.New("不支持的删除资源"))
		}
	}
	// Legacy channel point mirrors must not recreate explicitly removed data.
	for i := range p.Channels {
		p.Channels[i].Points = nil
	}
	p, err = core.NormalizeProject(p)
	if err != nil {
		return fail(err)
	}
	return p, result, nil
}

func Remove(path, expected string) error {
	if expected == "" {
		return errors.New("删除必须指定原文件修订号")
	}
	release, err := lock(path + ".lock")
	if err != nil {
		return err
	}
	defer release()
	initial, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !initial.Mode().IsRegular() {
		return errors.New("只允许删除普通工程文件，不接受符号链接")
	}
	doc, err := Read(path)
	if err != nil {
		return err
	}
	if doc.Revision != expected {
		return ErrConflict
	}
	latest, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !latest.Mode().IsRegular() || !os.SameFile(initial, latest) {
		return ErrConflict
	}
	return os.Remove(path)
}
