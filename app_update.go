package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const updateRepositoryAPI = "https://api.github.com/repos/Masterora/protocol-simulator"
const updateReleasePage = "https://github.com/Masterora/protocol-simulator/releases/latest"

// Use the same version as the packaged application metadata.
//
//go:embed wails.json
var updateBuildManifest []byte

type AppUpdate struct {
	Status         string `json:"status"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
}

func applicationVersion() string {
	var manifest struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	_ = json.Unmarshal(updateBuildManifest, &manifest)
	return manifest.Info.ProductVersion
}

var stableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func versionNumbers(value string) ([3]uint64, error) {
	var parts [3]uint64
	match := stableVersion.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return parts, errors.New("更新版本号无效")
	}
	for i := range parts {
		var err error
		parts[i], err = strconv.ParseUint(match[i+1], 10, 64)
		if err != nil {
			return parts, errors.New("更新版本号无效")
		}
	}
	return parts, nil
}

func readUpdateJSON(ctx context.Context, client *http.Client, endpoint, current string, value any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "protocol-simulator/"+current)
	response, err := client.Do(req)
	if err != nil {
		return 0, errors.New("无法检查更新，请检查网络后重试")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(value); err != nil {
			return response.StatusCode, errors.New("更新信息无法读取，请稍后重试")
		}
		return response.StatusCode, nil
	case http.StatusNotFound:
		return response.StatusCode, nil
	case http.StatusForbidden, http.StatusTooManyRequests:
		return response.StatusCode, errors.New("更新服务暂不可用或请求过于频繁，请稍后重试")
	default:
		return response.StatusCode, fmt.Errorf("更新服务返回 HTTP %d，请稍后重试", response.StatusCode)
	}
}

func checkApplicationUpdate(ctx context.Context, client *http.Client, repositoryAPI, current string) (AppUpdate, error) {
	result := AppUpdate{CurrentVersion: current}
	local, err := versionNumbers(current)
	if err != nil {
		return result, errors.New("当前应用版本号无效，无法检查更新")
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	status, err := readUpdateJSON(ctx, client, repositoryAPI+"/releases/latest", current, &release)
	if err != nil {
		return result, err
	}
	if status == http.StatusNotFound {
		// A missing release and an inaccessible repository both return 404.
		var repository struct {
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
		}
		status, err = readUpdateJSON(ctx, client, repositoryAPI, current, &repository)
		if err != nil {
			return result, err
		}
		if status != http.StatusOK || repository.Private || !strings.EqualFold(repository.FullName, "Masterora/protocol-simulator") {
			return result, errors.New("无法访问更新仓库，请稍后重试")
		}
		result.Status = "unpublished"
		return result, nil
	}
	if release.Draft || release.Prerelease {
		return result, errors.New("更新服务未返回正式发布版本")
	}
	remote, err := versionNumbers(release.Tag)
	if err != nil {
		return result, err
	}
	result.Status = "current"
	result.LatestVersion = release.Tag
	for i := range local {
		if remote[i] > local[i] {
			result.Status = "available"
			break
		}
		if remote[i] < local[i] {
			break
		}
	}
	return result, nil
}

func (a *App) CheckForUpdates() (AppUpdate, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return checkApplicationUpdate(ctx, &http.Client{Timeout: 10 * time.Second}, updateRepositoryAPI, applicationVersion())
}

func (a *App) OpenUpdatePage() error {
	if a.ctx == nil || a.remote != nil {
		return errors.New("请从主窗口打开更新")
	}
	runtime.BrowserOpenURL(a.ctx, updateReleasePage)
	return nil
}
