package main

import (
	"gytest-go/internal/core"
	"strings"
	"testing"
)

func TestPointCSVRoundTripPreservesAutomaticFeedbackLinks(t *testing.T) {
	points := []core.Point{{Address: 100, Kind: "yk", Name: "test,quoted", Feedback: "!1"}, {Address: 1, Kind: "yx", Name: "position", Value: 1}}
	data, err := encodePointCSV(points)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := decodePointCSV(data)
	if err != nil || len(roundtrip) != 2 || roundtrip[0].Feedback != "!1" || roundtrip[0].Name != "test,quoted" {
		t.Fatalf("lost association: %+v %v", roundtrip, err)
	}
	points[0].Feedback = ""
	data, err = encodePointCSV(points)
	if err != nil || strings.Contains(string(data), ",feedback") {
		t.Fatal("changed legacy CSV format without a link")
	}
	if _, err = decodePointCSV(data); err != nil {
		t.Fatal(err)
	}
	if _, err = decodePointCSV([]byte("address,kind,name,value,quality,other\n1,yx,n,1,0,x\n")); err == nil {
		t.Fatal("unexpected header accepted")
	}
}

func TestPointCSVRetainsWireMetadata(t *testing.T) {
	points := []core.Point{{Kind: "holding", Address: 2, Value: -12.5, DataType: "float32", ByteOrder: "CDAB", Tab: "yc"}, {Kind: "yx", Address: 1, Value: 2, WireType: 31, Timestamp: "2026-10-22T12:00:00.123Z", TimeInvalid: true}}
	data, err := encodePointCSV(points)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodePointCSV(data)
	if err != nil || len(got) != 2 || got[0].DataType != "float32" || got[0].ByteOrder != "CDAB" || got[1].WireType != 31 || got[1].Timestamp != points[1].Timestamp || !got[1].TimeInvalid {
		t.Fatal(got, err)
	}
}
