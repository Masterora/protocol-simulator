// Package pointtable reads and writes portable point tables without modifying a project.
package pointtable

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"gytest-go/internal/core"
)

const MaxBytes = 16 << 20
const MaxRows = 100000

var columns = []string{"kind", "address", "name", "value", "quality", "wireType", "dataType", "byteOrder", "tab", "timestamp", "timeInvalid", "feedback", "updated"}
var aliases = map[string]string{"类型": "kind", "点位类型": "kind", "点类型": "kind", "地址": "address", "点位": "address", "点号": "address", "信息体地址": "address", "描述": "name", "名称": "name", "点位名称": "name", "数值": "value", "值": "value", "值/状态": "value", "品质": "quality", "104类型": "wireType", "报文类型": "wireType", "数据类型": "dataType", "字节序": "byteOrder", "分类": "tab", "时间": "timestamp", "时标": "timestamp", "时间无效": "timeInvalid", "反馈": "feedback", "更新时间": "updated"}
var kinds = map[string]string{"遥信": "yx", "遥测": "yc", "遥控": "yk", "遥调": "yt", "累计量": "mc", "电度": "mc", "步位置": "position", "步调节": "step", "线圈": "coil", "离散量": "discrete", "离散输入": "discrete", "保持寄存器": "holding", "输入寄存器": "input"}

func Parse(name string, data []byte, defaultKind string) ([]core.Point, error) {
	if len(data) > MaxBytes {
		return nil, errors.New("点表不能超过16MB")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".json" {
		var points []core.Point
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if err := d.Decode(&points); err != nil {
			return nil, fmt.Errorf("JSON点表必须是点位数组：%w", err)
		}
		if d.Decode(new(any)) != io.EOF {
			return nil, errors.New("JSON点表含有多余内容")
		}
		return validate(points)
	}
	var records [][]string
	if ext == ".xlsx" {
		var err error
		records, err = readXLSX(data)
		if err != nil {
			return nil, err
		}
	} else {
		if ext != ".csv" && ext != ".tsv" && ext != ".txt" {
			return nil, errors.New("请选择CSV、TSV、JSON或XLSX点表")
		}
		if !utf8.Valid(data) {
			return nil, errors.New("点表不是UTF-8，请在Excel中另存为CSV UTF-8，或使用XLSX")
		}
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
		r.FieldsPerRecord = -1
		if ext == ".tsv" || ext == ".txt" {
			r.Comma = '\t'
		}
		for {
			row, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("点表读取失败：%w", err)
			}
			records = append(records, row)
			if len(records) > MaxRows+1 {
				return nil, errors.New("一次最多导入100000个点位")
			}
		}
	}
	if len(records) < 2 {
		return nil, errors.New("点表需要表头和至少一个点位")
	}
	fields := make([]string, len(records[0]))
	seen := map[string]bool{}
	for i, name := range records[0] {
		name = strings.TrimSpace(strings.TrimPrefix(name, "\ufeff"))
		if v, ok := aliases[name]; ok {
			name = v
		}
		for _, v := range columns {
			if strings.EqualFold(name, v) {
				fields[i] = v
			}
		}
		if fields[i] != "" {
			if seen[fields[i]] {
				return nil, fmt.Errorf("表头重复：%s", name)
			}
			seen[fields[i]] = true
		}
	}
	if !seen["address"] {
		return nil, errors.New("未找到地址列，请使用address、地址或点号")
	}
	if !seen["kind"] && defaultKind == "" {
		return nil, errors.New("没有类型列，请选择默认点位类型")
	}
	points := make([]core.Point, 0, len(records)-1)
	for index, row := range records[1:] {
		blank := true
		for _, v := range row {
			if strings.TrimSpace(v) != "" {
				blank = false
			}
		}
		if blank {
			continue
		}
		values := map[string]any{"kind": defaultKind}
		for i, key := range fields {
			if key == "" || i >= len(row) {
				continue
			}
			v := strings.TrimSpace(row[i])
			if key == "name" || key == "feedback" {
				v = row[i]
			}
			if v == "" {
				continue
			}
			var value any = v
			switch key {
			case "kind", "tab":
				if translated, ok := kinds[v]; ok {
					value = translated
				}
			case "address", "quality", "wireType":
				n, err := unsigned(v)
				if err != nil {
					return nil, fmt.Errorf("第%d行%s：%w", index+2, key, err)
				}
				value = n
			case "value":
				n, err := strconv.ParseFloat(v, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return nil, fmt.Errorf("第%d行数值无效", index+2)
				}
				value = n
			case "timeInvalid":
				switch strings.ToLower(v) {
				case "true", "1", "是":
					value = true
				case "false", "0", "否":
					value = false
				default:
					return nil, fmt.Errorf("第%d行时间无效标志应为true/false", index+2)
				}
			}
			if ext != ".xlsx" {
				if text, ok := value.(string); ok && len(text) > 1 && text[0] == '\'' && strings.ContainsRune("'=+-@", rune(text[1])) {
					value = text[1:]
				}
			}
			values[key] = value
		}
		encoded, _ := json.Marshal(values)
		var p core.Point
		if err := json.Unmarshal(encoded, &p); err != nil {
			return nil, fmt.Errorf("第%d行字段越界：%w", index+2, err)
		}
		points = append(points, p)
	}
	return validate(points)
}
func unsigned(s string) (uint64, error) {
	base := 10
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base = 16
		s = s[2:]
	} else if strings.HasSuffix(strings.ToLower(s), "h") {
		base = 16
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseUint(s, base, 32)
	if err != nil {
		return 0, errors.New("应为十进制地址或0x开头的十六进制数")
	}
	return n, nil
}
func validate(points []core.Point) ([]core.Point, error) {
	if len(points) == 0 || len(points) > MaxRows {
		return nil, errors.New("点表须包含1–100000个点位")
	}
	seen := map[core.PointKey]bool{}
	for i, p := range points {
		switch p.Kind {
		case "yx", "yc", "yk", "yt", "mc", "position", "step", "coil", "discrete", "holding", "input":
		default:
			return nil, fmt.Errorf("第%d个点位类型无效：%s", i+1, p.Kind)
		}
		key := core.PointKey{Kind: p.Kind, Address: p.Address}
		if seen[key] {
			return nil, fmt.Errorf("点位重复：%s/%d", p.Kind, p.Address)
		}
		seen[key] = true
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			return nil, fmt.Errorf("点位%s/%d的数值无效", p.Kind, p.Address)
		}
	}
	return points, nil
}
func Export(format string, points []core.Point) ([]byte, error) {
	if len(points) > MaxRows {
		return nil, errors.New("一次最多导出100000个点位")
	}
	if format == "json" {
		b, err := json.MarshalIndent(points, "", "  ")
		if len(b) > MaxBytes {
			return nil, errors.New("导出文件超过16MB，请分批导出")
		}
		return b, err
	}
	records := [][]string{columns}
	for _, p := range points {
		data, _ := json.Marshal(p)
		var obj map[string]any
		_ = json.Unmarshal(data, &obj)
		row := make([]string, len(columns))
		for i, k := range columns {
			if v, ok := obj[k]; ok {
				switch n := v.(type) {
				case float64:
					row[i] = strconv.FormatFloat(n, 'f', -1, 64)
				default:
					row[i] = fmt.Sprint(n)
				}
			}
		}
		if format == "csv" || format == "tsv" {
			for i, key := range columns {
				switch key {
				case "address", "value", "quality", "wireType", "timeInvalid":
					continue
				}
				if row[i] != "" && strings.ContainsRune("'=+-@", rune(row[i][0])) {
					row[i] = "'" + row[i]
				}
			}
		}
		records = append(records, row)
	}
	if format == "xlsx" {
		return writeXLSX(records)
	}
	if format != "csv" && format != "tsv" {
		return nil, errors.New("导出格式无效")
	}
	var b bytes.Buffer
	b.WriteString("\ufeff")
	w := csv.NewWriter(&b)
	if format == "tsv" {
		w.Comma = '\t'
	}
	w.UseCRLF = true
	if err := w.WriteAll(records); err != nil {
		return nil, err
	}
	if b.Len() > MaxBytes {
		return nil, errors.New("导出文件超过16MB，请分批导出")
	}
	return b.Bytes(), nil
}
