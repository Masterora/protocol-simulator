// Build-time resource writer shared by Windows and macOS cross-builds.
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func versionParts(text string) ([4]uint16, error) {
	var result [4]uint16
	parts := strings.Split(text, ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("version must have three numeric components: %q", text)
	}
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 16)
		if err != nil || strconv.FormatUint(n, 10) != part {
			return result, fmt.Errorf("invalid Windows version component: %q", part)
		}
		result[i] = uint16(n)
	}
	return result, nil
}

func main() {
	if len(os.Args) != 3 {
		panic("usage: windows-resources ROOT EXECUTABLE")
	}
	root, binaryPath := os.Args[1], os.Args[2]
	var manifest struct {
		Info struct {
			ProductName    string `json:"productName"`
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	data, err := os.ReadFile(filepath.Join(root, "wails.json"))
	must(err)
	must(json.Unmarshal(data, &manifest))
	parts, err := versionParts(manifest.Info.ProductVersion)
	must(err)
	textVersion := manifest.Info.ProductVersion + ".0"
	icon, err := os.ReadFile(filepath.Join(root, "build/windows/icon.ico"))
	must(err)
	appManifest, err := os.ReadFile(filepath.Join(root, "build/windows/app.manifest"))
	must(err)
	if strings.Count(string(appManifest), "{{.Version}}") != 1 {
		panic("application manifest must contain one version placeholder")
	}
	appManifest = []byte(strings.ReplaceAll(string(appManifest), "{{.Version}}", textVersion))
	rs := winres.ResourceSet{}
	count := int(binary.LittleEndian.Uint16(icon[4:]))
	group := append([]byte{}, icon[:6]...)
	for i := 0; i < count; i++ {
		entry := icon[6+16*i:]
		size, offset := binary.LittleEndian.Uint32(entry[8:]), binary.LittleEndian.Uint32(entry[12:])
		must(rs.Set(winres.RT_ICON, winres.ID(i+1), 1033, icon[offset:offset+size]))
		group = append(group, entry[:12]...)
		group = append(group, byte(i+1), byte((i+1)>>8))
	}
	// Group 1 is used by Explorer, group 3 by Wails native windows.
	for _, id := range []winres.ID{1, 3} {
		must(rs.Set(winres.RT_GROUP_ICON, id, 1033, append([]byte{}, group...)))
	}
	must(rs.Set(winres.RT_MANIFEST, winres.ID(1), 1033, appManifest))
	v := version.Info{FileVersion: parts, ProductVersion: parts}
	for key, value := range map[string]string{
		"FileDescription": manifest.Info.ProductName, "ProductName": manifest.Info.ProductName,
		"InternalName": "protocol-simulator", "OriginalFilename": "protocol-simulator.exe",
		"FileVersion": textVersion, "ProductVersion": textVersion,
	} {
		must(v.Set(1033, key, value))
	}
	rs.SetVersionInfo(v)
	src, err := os.Open(binaryPath)
	must(err)
	stage := binaryPath + ".resource-stage"
	dst, err := os.Create(stage)
	must(err)
	defer os.Remove(stage)
	err = rs.WriteToEXE(dst, src)
	closeErr := dst.Close()
	src.Close()
	must(err)
	must(closeErr)
	must(os.Rename(stage, binaryPath))
	fmt.Printf("Windows resources written: version %s; icon groups 1,3; %d images\n", textVersion, count)
}
