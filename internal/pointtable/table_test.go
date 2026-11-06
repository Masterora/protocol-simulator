package pointtable

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"gytest-go/internal/core"
)

func TestPortableTablesRoundTripMetadata(t *testing.T) {
	points := []core.Point{{Kind: "yx", Address: 0, Name: "=开关,出口\n状态", Value: 1, Quality: 128, WireType: 30, Timestamp: "2026-11-03T12:13:14+08:00", TimeInvalid: true, Tab: "yx", Feedback: "!2"}, {Kind: "yc", Address: 0xffffff, Name: "'温度 \"A\"", Value: 23.125, WireType: 13, Quality: 0, DataType: "float32", ByteOrder: "CDAB", Updated: "12:13:14.100"}}
	for _, format := range []string{"csv", "tsv", "json", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			data, err := Export(format, points)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Parse("points."+format, data, "")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, points) {
				t.Fatalf("round trip lost metadata: %#v", got)
			}
		})
	}
}
func TestImportChineseHeadersAndAtomicRejection(t *testing.T) {
	data := []byte("类型,点号,描述,数值,品质\n遥测,4001H,温度,23.5,0\n")
	points, err := Parse("a.csv", data, "")
	if err != nil || points[0].Address != 0x4001 || points[0].Kind != "yc" {
		t.Fatal(points, err)
	}
	for _, s := range []string{"类型,点号\n遥测,1\n遥测,1\n", "类型,点号,品质\n遥信,1,256\n", "类型,点号,数值\n遥测,1,NaN\n", "类型,地址,点号\n遥信,1,2\n", "类型,点号\n未知,1\n"} {
		if _, err := Parse("a.csv", []byte(s), ""); err == nil {
			t.Fatal("invalid table accepted", s)
		}
	}
	if _, err := Parse("a.json", []byte(`[{"kind":"yc","address":1}] true`), ""); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	p, err := Parse("a.tsv", []byte("地址\t描述\n0x10\t开关\n"), "yx")
	if err != nil || p[0].Kind != "yx" {
		t.Fatal(p, err)
	}
}
func TestSpreadsheetSharedStringsSparseCellsAndFormulaRejection(t *testing.T) {
	makeBook := func(sheet string) []byte {
		original, err := writeXLSX([][]string{{"kind", "address", "name"}, {"yx", "1", "A"}})
		if err != nil {
			t.Fatal(err)
		}
		zr, _ := zip.NewReader(bytes.NewReader(original), int64(len(original)))
		var out bytes.Buffer
		zw := zip.NewWriter(&out)
		for _, f := range zr.File {
			w, _ := zw.Create(f.Name)
			if f.Name == "xl/worksheets/sheet1.xml" {
				w.Write([]byte(sheet))
			} else {
				r, _ := f.Open()
				buf := new(bytes.Buffer)
				buf.ReadFrom(r)
				r.Close()
				w.Write(buf.Bytes())
			}
		}
		w, _ := zw.Create("xl/sharedStrings.xml")
		w.Write([]byte(`<sst><si><t>kind</t></si><si><t>address</t></si><si><t>name</t></si><si><t>yx</t></si><si><r><t>开</t></r><r><t>关</t></r></si></sst>`))
		zw.Close()
		return out.Bytes()
	}
	sheet := `<worksheet><sheetData><row><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c><c r="D1" t="s"><v>2</v></c></row><row><c r="A2" t="s"><v>3</v></c><c r="C2"><v>16</v></c><c r="D2" t="s"><v>4</v></c></row></sheetData></worksheet>`
	p, err := Parse("a.xlsx", makeBook(sheet), "")
	if err != nil || p[0].Address != 16 || p[0].Name != "开关" {
		t.Fatal(p, err)
	}
	if _, err := Parse("a.xlsx", makeBook(strings.Replace(sheet, "<v>16</v>", "<f>1+15</f><v>16</v>", 1)), ""); err == nil {
		t.Fatal("formula silently accepted")
	}
	// A declared uncompressed size above the cap must fail before inflating XML.
	b := makeBook(sheet)
	for i := 0; i+46 < len(b); i++ {
		if bytes.Equal(b[i:i+4], []byte{'P', 'K', 1, 2}) {
			binary.LittleEndian.PutUint32(b[i+24:], MaxBytes+1)
			break
		}
	}
	if _, err := Parse("a.xlsx", b, ""); err == nil {
		t.Fatal("oversized ZIP accepted")
	}
}

func TestSpreadsheetExportRejectsUnsupportedControlsAndEscapesLiteralCSV(t *testing.T) {
	if _, err := Export("xlsx", []core.Point{{Kind: "yx", Address: 1, Name: "bad\x01name"}}); err == nil {
		t.Fatal("invalid XML text was silently changed")
	}
	points := []core.Point{{Kind: "yx", Address: 1, Name: "=1+2"}, {Kind: "yx", Address: 2, Name: "'=literal"}, {Kind: "yx", Address: 3, Name: "+sum"}}
	b, err := Export("csv", points)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("'=1+2")) {
		t.Fatal("spreadsheet expression was not escaped")
	}
	got, err := Parse("a.csv", b, "")
	if err != nil || !reflect.DeepEqual(got, points) {
		t.Fatal(got, err)
	}
}
