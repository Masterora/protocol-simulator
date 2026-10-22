package core

import "testing"

func TestOriginalConfigurationBuildsIndependentPointTables(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "通道0", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	config := &DeviceConfiguration{Version: "TCP版", Values: map[string]string{"txtAdd": "1", "txtYX_Add": "1", "txtYX_Num": "18"}, Ranges: []PointRange{{Tab: "yx", Kind: "discrete", Address: 1, Count: 18, Name: "遥信"}, {Tab: "yc", Kind: "holding", Address: 1024, Count: 18, Name: "遥测"}, {Tab: "mc", Kind: "input", Address: 2048, Count: 18, Name: "脉冲"}, {Tab: "yk", Kind: "coil", Address: 2560, Count: 18, Name: "遥控"}}}
	deviceID, err := e.UpsertDevice(id, "modbus-tcp", Device{Name: "Modbus", Unit: 1, CommonAddress: 1, Configuration: config}, false)
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.Device(id, deviceID)
	if err != nil || len(d.Points) != 72 || d.Points[18].Address != 1024 {
		t.Fatalf("configuration did not build point table: %+v %v", d, err)
	}
	config.Values["txtAdd"] = "99"
	d.Configuration.Ranges[0].Address = 77
	snapshot := e.Snapshot()
	snapshot.Project.Channels[0].Devices[0].Configuration.Values["txtAdd"] = "88"
	stored, _ := e.Device(id, deviceID)
	if stored.Configuration.Values["txtAdd"] != "1" || stored.Configuration.Ranges[0].Address != 1 {
		t.Fatal("configuration was not deeply copied")
	}
	second, err := e.UpsertDevice(id, "modbus-tcp", Device{Name: "Modbus", Unit: 2, CommonAddress: 1, Configuration: stored.Configuration}, false)
	if err != nil {
		t.Fatal(err)
	}
	e.applyDevicePoints(id, deviceID, []Point{{Kind: "holding", Address: 1024, Value: 666}})
	stored, _ = e.Device(id, deviceID)
	if _, err = e.UpsertDevice(id, "modbus-tcp", stored, false); err != nil {
		t.Fatal(err)
	}
	stored, _ = e.Device(id, deviceID)
	other, _ := e.Device(id, second)
	if stored.Points[18].Value != 666 || other.Points[18].Value != 0 {
		t.Fatal("configuration editing overwrote live data or another device")
	}
	defer e.StopAll()
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	stored.Configuration.Ranges[0].Address = 65535
	stored.Configuration.Ranges[0].Count = 2
	if _, err = e.UpsertDevice(id, "modbus-tcp", stored, false); err == nil {
		t.Fatal("overflowing point range accepted")
	}
	if e.Snapshot().States[0].Status != "listening" {
		t.Fatal("invalid configuration stopped current connection")
	}
}

func TestRTUOverTCPPreservesIndependentCommunicationConfiguration(t *testing.T) {
	e, server, client := multiPair(t, "modbus-rtu")
	for _, id := range []string{"one", "two"} {
		if err := e.ReadDeviceModbus(client, id, 3, 0, 1); err != nil {
			t.Fatal(err)
		}
		expected := 111.0
		if id == "two" {
			expected = 222
		}
		eventually(t, func() bool { return deviceValue(e, client, id) == expected })
	}
	if err := e.WriteDeviceModbus(client, "two", 6, 0, 444); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return deviceValue(e, server, "two") == 444 })
	if deviceValue(e, server, "one") != 111 {
		t.Fatal("RTU write changed another device")
	}
	if got := e.Snapshot().Project.Channels[0].Transport; got != "tcp-server" {
		t.Fatal("adding RTU devices changed communication settings")
	}
}
