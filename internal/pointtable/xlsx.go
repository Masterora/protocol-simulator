package pointtable

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

type richText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (r richText) value() string {
	s := r.Text
	for _, v := range r.Runs {
		s += v.Text
	}
	return s
}
func readXLSX(data []byte) ([][]string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("XLSX不是有效的Excel工作簿")
	}
	if len(z.File) > 2000 {
		return nil, errors.New("Excel内部文件过多")
	}
	files := map[string]*zip.File{}
	var size uint64
	for _, f := range z.File {
		if f.UncompressedSize64 > MaxBytes {
			return nil, errors.New("Excel内部文件超过16MB")
		}
		size += f.UncompressedSize64
		if size > MaxBytes {
			return nil, errors.New("Excel展开后超过16MB")
		}
		if files[f.Name] != nil {
			return nil, errors.New("Excel存在重复内部文件")
		}
		files[f.Name] = f
	}
	read := func(name string, out any) error {
		f := files[name]
		if f == nil {
			return fmt.Errorf("Excel缺少%s", name)
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		defer r.Close()
		return xml.NewDecoder(io.LimitReader(r, MaxBytes+1)).Decode(out)
	}
	var workbook struct {
		Sheets []struct {
			ID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := read("xl/workbook.xml", &workbook); err != nil {
		return nil, err
	}
	if len(workbook.Sheets) == 0 {
		return nil, errors.New("Excel中没有工作表")
	}
	var rels struct {
		Rows []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := read("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	sheet := ""
	for _, v := range rels.Rows {
		if v.ID == workbook.Sheets[0].ID && v.Mode != "External" {
			if strings.HasPrefix(v.Target, "/") {
				sheet = strings.TrimPrefix(v.Target, "/")
			} else {
				sheet = path.Clean(path.Join("xl", v.Target))
			}
		}
	}
	if !strings.HasPrefix(sheet, "xl/") || !strings.HasSuffix(sheet, ".xml") {
		return nil, errors.New("Excel工作表路径无效")
	}
	var stringsXML struct {
		Values []richText `xml:"si"`
	}
	if files["xl/sharedStrings.xml"] != nil {
		if err := read("xl/sharedStrings.xml", &stringsXML); err != nil {
			return nil, err
		}
	}
	var sheetXML struct {
		Rows []struct {
			Cells []struct {
				Ref     string   `xml:"r,attr"`
				Type    string   `xml:"t,attr"`
				Value   string   `xml:"v"`
				Inline  richText `xml:"is"`
				Formula *string  `xml:"f"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := read(sheet, &sheetXML); err != nil {
		return nil, err
	}
	if len(sheetXML.Rows) > MaxRows+1 {
		return nil, errors.New("Excel最多100000个点位")
	}
	result := make([][]string, 0, len(sheetXML.Rows))
	for ri, row := range sheetXML.Rows {
		cells := []string{}
		for ci, c := range row.Cells {
			if c.Formula != nil {
				return nil, fmt.Errorf("Excel第%d行含公式，请先粘贴为数值后导入", ri+1)
			}
			index := ci
			if c.Ref != "" {
				index = 0
				n := 0
				for _, v := range strings.ToUpper(c.Ref) {
					if v < 'A' || v > 'Z' {
						break
					}
					index = index*26 + int(v-'A'+1)
					n++
				}
				if n == 0 || index < 1 || index > 256 {
					return nil, errors.New("Excel列地址无效或超过256列")
				}
				index--
			}
			if index > 255 {
				return nil, errors.New("Excel最多256列")
			}
			for len(cells) <= index {
				cells = append(cells, "")
			}
			value := c.Value
			switch c.Type {
			case "s":
				n, e := strconv.Atoi(c.Value)
				if e != nil || n < 0 || n >= len(stringsXML.Values) {
					return nil, errors.New("Excel共享文本索引无效")
				}
				value = stringsXML.Values[n].value()
			case "inlineStr":
				value = c.Inline.value()
			case "b":
				if value == "1" {
					value = "true"
				} else if value == "0" {
					value = "false"
				} else {
					return nil, errors.New("Excel布尔值无效")
				}
			case "e":
				return nil, fmt.Errorf("Excel第%d行包含错误单元格", ri+1)
			}
			cells[index] = value
		}
		result = append(result, cells)
	}
	return result, nil
}

type worksheetWriter struct {
	w   io.Writer
	n   int
	err error
}

func (w *worksheetWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.n+len(p) > MaxBytes-8192 {
		w.err = errors.New("Excel展开后超过16MB，请分批导出")
		return 0, w.err
	}
	n, err := w.w.Write(p)
	w.n += n
	w.err = err
	return n, err
}
func writeXLSX(rows [][]string) ([]byte, error) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	entries := map[string]string{
		"[Content_Types].xml":        `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="点表" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
	}
	for name, s := range entries {
		w, e := z.Create(name)
		if e != nil {
			return nil, e
		}
		if _, e = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+s); e != nil {
			return nil, e
		}
	}
	w, err := z.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	limited := &worksheetWriter{w: w}
	w = limited
	if _, err = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`); err != nil {
		return nil, err
	}
	for i, row := range rows {
		fmt.Fprintf(w, `<row r="%d">`, i+1)
		for _, value := range row {
			for _, r := range value {
				if r < 32 && r != 9 && r != 10 && r != 13 || r == 0xfffe || r == 0xffff {
					return nil, errors.New("点表含Excel不支持的控制字符，请使用JSON导出")
				}
			}
			io.WriteString(w, `<c t="inlineStr"><is><t xml:space="preserve">`)
			if err = xml.EscapeText(w, []byte(value)); err != nil {
				return nil, err
			}
			io.WriteString(w, `</t></is></c>`)
		}
		io.WriteString(w, `</row>`)
	}
	io.WriteString(w, `</sheetData></worksheet>`)
	if limited.err != nil {
		return nil, limited.err
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	if b.Len() > MaxBytes {
		return nil, errors.New("导出文件超过16MB")
	}
	return b.Bytes(), nil
}
