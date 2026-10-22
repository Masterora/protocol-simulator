//go:build ignore

// Cross-platform resource writer. Windows releases still use resources.ps1;
// this helper reads that same script's version fields and uses the same ICO and
// manifest when building a Windows executable on macOS.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf16"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func encoded(s string) []byte {
	out := []byte{}
	for _, r := range utf16.Encode([]rune(s)) {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}
func main() {
	root := os.Args[2]
	binaryPath := filepath.Join(root, "build/bin/protocol-simulator.exe")
	icoBytes, err := os.ReadFile(filepath.Join(root, "build/windows/icon.ico"))
	must(err)
	manifest, err := os.ReadFile(filepath.Join(root, "build/windows/app.manifest"))
	must(err)
	spec, err := os.ReadFile(filepath.Join(root, "build/windows/resources.ps1"))
	must(err)
	fields := regexp.MustCompile(`Text\("([^"]+)","([^"]+)"\)`).FindAllSubmatch(spec, -1)
	if len(fields) < 6 {
		panic("Missing resource version fields")
	}
	if os.Args[1] == "apply" {
		rs := winres.ResourceSet{}
		count := int(binary.LittleEndian.Uint16(icoBytes[4:]))
		group := append([]byte{}, icoBytes[:6]...)
		for i := 0; i < count; i++ {
			entry := icoBytes[6+16*i:]
			n, offset := binary.LittleEndian.Uint32(entry[8:]), binary.LittleEndian.Uint32(entry[12:])
			must(rs.Set(winres.RT_ICON, winres.ID(i+1), 1033, icoBytes[offset:offset+n]))
			group = append(group, entry[:12]...)
			group = append(group, byte(i+1), byte((i+1)>>8))
		}
		for _, id := range []winres.ID{1, 3} {
			must(rs.Set(winres.RT_GROUP_ICON, id, 1033, append([]byte{}, group...)))
		}
		must(rs.Set(winres.RT_MANIFEST, winres.ID(1), 1033, manifest))
		v := version.Info{FileVersion: [4]uint16{1, 0, 0, 0}, ProductVersion: [4]uint16{1, 0, 0, 0}}
		for _, f := range fields {
			must(v.Set(1033, string(f[1]), string(f[2])))
		}
		rs.SetVersionInfo(v)
		src, err := os.Open(binaryPath)
		must(err)
		dst, err := os.Create(binaryPath + ".resource-stage")
		must(err)
		err = rs.WriteToEXE(dst, src)
		dst.Close()
		src.Close()
		must(err)
		must(os.Rename(binaryPath+".resource-stage", binaryPath))
	}
	f, err := os.Open(binaryPath)
	must(err)
	defer f.Close()
	rs, err := winres.LoadFromEXE(f)
	must(err)
	if !bytes.Equal(rs.Get(winres.RT_MANIFEST, winres.ID(1), 1033), manifest) {
		panic("Manifest mismatch")
	}
	ver := rs.Get(winres.RT_VERSION, winres.ID(1), 1033)
	for _, field := range fields {
		if !bytes.Contains(ver, encoded(string(field[2]))) {
			panic("Version field mismatch")
		}
	}
	count := int(binary.LittleEndian.Uint16(icoBytes[4:]))
	for _, id := range []winres.ID{1, 3} {
		group := rs.Get(winres.RT_GROUP_ICON, id, 1033)
		if len(group) < 6 || int(binary.LittleEndian.Uint16(group[4:])) != count {
			panic("Icon count mismatch")
		}
		for i := 0; i < count; i++ {
			entry := group[6+14*i:]
			resourceID := binary.LittleEndian.Uint16(entry[12:])
			image := rs.Get(winres.RT_ICON, winres.ID(resourceID), 1033)
			matched := false
			for j := 0; j < count; j++ {
				e := icoBytes[6+j*16:]
				n, offset := binary.LittleEndian.Uint32(e[8:]), binary.LittleEndian.Uint32(e[12:])
				if bytes.Equal(image, icoBytes[offset:offset+n]) {
					matched = true
				}
			}
			if !matched {
				panic("ICO image bytes mismatch")
			}
		}
	}
	fmt.Printf("Windows resources verified: icon groups 1,3; %d images; version fields; manifest\n", count)
}
