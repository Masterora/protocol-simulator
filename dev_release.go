//go:build !devassets

package main

import "io/fs"

type DevelopmentState struct {
	Enabled bool   `json:"enabled"`
	Code    string `json:"code"`
	Styles  string `json:"styles"`
}

func developmentAssets(embedded fs.FS) (fs.FS, error)       { return embedded, nil }
func (*App) GetDevelopmentState() (DevelopmentState, error) { return DevelopmentState{}, nil }
func developmentProjectLaunch() bool                        { return false }

func (*App) ReportDevelopmentReload(string, string) {}
