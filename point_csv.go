package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"gytest-go/internal/core"
	"strconv"
	"strings"
)

func encodePointCSV(points []core.Point) ([]byte, error) {
	extended := false
	linked := false
	for _, p := range points {
		linked = linked || p.Feedback != ""
		extended = extended || p.WireType != 0 || p.Timestamp != "" || p.DataType != "" || p.ByteOrder != "" || p.Tab != ""
	}
	header := []string{"address", "kind", "name", "value", "quality"}
	if linked {
		header = append(header, "feedback")
	}
	if extended {
		header = append(header, "tab", "wireType", "timestamp", "timeInvalid", "dataType", "byteOrder")
	}
	var b bytes.Buffer
	b.WriteString("\ufeff")
	w := csv.NewWriter(&b)
	_ = w.Write(header)
	for _, p := range points {
		row := []string{strconv.FormatUint(uint64(p.Address), 10), p.Kind, p.Name, strconv.FormatFloat(p.Value, 'g', -1, 64), strconv.Itoa(int(p.Quality))}
		if linked {
			row = append(row, p.Feedback)
		}
		if extended {
			row = append(row, p.Tab, strconv.Itoa(int(p.WireType)), p.Timestamp, strconv.FormatBool(p.TimeInvalid), p.DataType, p.ByteOrder)
		}
		_ = w.Write(row)
	}
	w.Flush()
	return b.Bytes(), w.Error()
}
func decodePointCSV(data []byte) ([]core.Point, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("点表为空")
	}
	if len(rows[0]) < 5 || strings.Join(rows[0][:5], ",") != "address,kind,name,value,quality" {
		return nil, errors.New("表头应以 address,kind,name,value,quality 开始")
	}
	allowed := map[string]bool{"feedback": true, "tab": true, "wireType": true, "timestamp": true, "timeInvalid": true, "dataType": true, "byteOrder": true}
	seen := map[string]bool{}
	for _, key := range rows[0][5:] {
		if !allowed[key] || seen[key] {
			return nil, errors.New("可选列名称无效或重复")
		}
		seen[key] = true
	}
	points := make([]core.Point, 0, len(rows)-1)
	for i, row := range rows[1:] {
		if len(row) != len(rows[0]) {
			return nil, fmt.Errorf("第%d行列数不正确", i+2)
		}
		address, e1 := strconv.ParseUint(strings.TrimSpace(row[0]), 10, 24)
		value, e2 := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
		quality, e3 := strconv.ParseUint(strings.TrimSpace(row[4]), 10, 8)
		if e1 != nil || e2 != nil || e3 != nil {
			return nil, fmt.Errorf("第%d行数值格式不正确", i+2)
		}
		p := core.Point{Address: uint32(address), Kind: strings.TrimSpace(row[1]), Name: row[2], Value: value, Quality: byte(quality)}
		for j, key := range rows[0][5:] {
			v := strings.TrimSpace(row[j+5])
			switch key {
			case "feedback":
				p.Feedback = v
			case "tab":
				p.Tab = v
			case "dataType":
				p.DataType = v
			case "byteOrder":
				p.ByteOrder = v
			case "timestamp":
				p.Timestamp = v
			case "timeInvalid":
				x, err := strconv.ParseBool(v)
				if err != nil {
					return nil, fmt.Errorf("第%d行时间品质无效", i+2)
				}
				p.TimeInvalid = x
			case "wireType":
				x, err := strconv.ParseUint(v, 10, 8)
				if err != nil {
					return nil, fmt.Errorf("第%d行类型无效", i+2)
				}
				p.WireType = byte(x)
			}
		}
		points = append(points, p)
	}
	return points, nil
}
