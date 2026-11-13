package runner

import (
	"context"
	"errors"

	"gytest-go/internal/core"
	"gytest-go/internal/projectfile"
)

func (s *configurationState) commitTool(ctx context.Context, next core.Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	revision, err := projectfile.Commit(s.doc.Path, s.doc.Revision, next)
	if err != nil {
		return err
	}
	s.publish(projectfile.Document{Project: next, Revision: revision, Path: s.doc.Path})
	return nil
}

func (s *configurationState) configurePointTable(ctx context.Context, req projectfile.Request) (projectfile.View, projectfile.Result, error) {
	view := projectfile.View{Path: s.doc.Path, Revision: s.doc.Revision, Resource: req.Resource}
	result := projectfile.Result{Path: s.doc.Path, Revision: s.doc.Revision, Channel: req.Channel, Device: req.Device}
	if req.Revision == "" || req.Revision != s.doc.Revision {
		return view, result, projectfile.ErrConflict
	}
	if req.Action != "preview" && req.Action != "import" {
		return view, result, errors.New("无效点表操作")
	}
	var input projectfile.PointTableImport
	if err := projectfile.ToolPayload(req.Fields, "table", &input); err != nil {
		return view, result, err
	}
	next, summary, err := projectfile.PointTableCandidate(s.doc.Project, req.Channel, req.Device, input)
	if err != nil {
		return view, result, err
	}
	if req.Action == "preview" {
		view.Value = projectfile.Object(summary)
		return view, result, nil
	}
	hint := core.ServerPointHint{Channel: req.Channel, Device: req.Device, Imported: make(map[core.PointKey]bool, len(input.Rows))}
	for _, p := range input.Rows {
		hint.Imported[core.PointKey{Kind: p.Kind, Address: p.Address}] = true
	}
	effect, err := s.engine.ApplyServerConfiguration(s.doc.Project, next, hint, func() error {
		return s.commitTool(ctx, next)
	})
	if err != nil {
		return view, result, err
	}
	result.Saved, result.Applied, result.Revision, result.Message = true, true, s.doc.Revision, "点表已保存并生效"
	s.sink.info("configuration_saved", "resource", req.Resource, "action", req.Action, "path", s.doc.Path, "revision", s.doc.Revision)
	reset := map[string]bool{}
	for _, d := range effect.ResetDevices {
		reset[dataKey(d.Channel, d.Device)] = true
	}
	return view, result, s.rebuildPlans(reset, nil)
}
