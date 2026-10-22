package core

import (
	"os"
	"testing"
	"time"
)

// The macOS verification runner connects two disposable PTYs. This exercises
// the real serial driver, framing, read/write and cancellation paths, without
// requiring or changing a user's physical serial devices.
func TestSerialTransportLoopback(t *testing.T) {
	portA, portB := os.Getenv("GYLAB_SERIAL_A"), os.Getenv("GYLAB_SERIAL_B")
	if portA == "" || portB == "" {
		t.Skip("requires the disposable PTY pair from scripts/verify_serial_macos.py")
	}
	for _, protocol := range []string{"raw", "modbus-rtu"} {
		t.Run(protocol, func(t *testing.T) {
			e := NewEngine()
			t.Cleanup(e.StopAll)
			server := Channel{Name: "serial server", Protocol: protocol, Role: "server", Transport: "serial", Address: portA}
			client := Channel{Name: "serial client", Protocol: protocol, Role: "client", Transport: "serial", Address: portB}
			if protocol == "modbus-rtu" {
				server.Devices, client.Devices = twoDevices(protocol, false), twoDevices(protocol, false)
				for i := range server.Devices {
					server.Devices[i].Points = []Point{{Kind: "holding", Address: 0, Value: float64(111 * (i + 1))}, {Kind: "input", Address: 0, Value: 17}, {Kind: "coil", Address: 0, Value: 1}, {Kind: "discrete", Address: 0, Value: 1}}
				}
			}
			sid, err := e.UpsertChannel(server)
			if err != nil {
				t.Fatal(err)
			}
			cid, err := e.UpsertChannel(client)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{sid, cid} {
				if err = e.Start(id); err != nil {
					t.Fatal(err)
				}
				waitConnected(t, e, id)
			}
			if protocol == "raw" {
				for _, id := range []string{sid, cid} {
					if err = e.SendHex(id, "CA FE 00 FF", false); err != nil {
						t.Fatal(err)
					}
				}
				eventually(t, func() bool {
					seen := map[string]bool{}
					for _, m := range e.Snapshot().Messages {
						if m.Direction == "RX" && m.Hex == "CAFE00FF" {
							seen[m.ChannelID] = true
						}
					}
					return seen[sid] && seen[cid]
				})
			} else {
				value := func(channel, device, kind string) float64 {
					d, _ := e.Device(channel, device)
					for _, p := range d.Points {
						if p.Kind == kind && p.Address == 0 {
							return p.Value
						}
					}
					return -1
				}
				for _, device := range []string{"one", "two"} {
					for _, read := range []struct {
						fc   byte
						kind string
						want float64
					}{{1, "coil", 1}, {2, "discrete", 1}, {3, "holding", 111}, {4, "input", 17}} {
						want := read.want
						if device == "two" && read.kind == "holding" {
							want = 222
						}
						if err = e.ReadDeviceModbus(cid, device, read.fc, 0, 1); err != nil {
							t.Fatal(err)
						}
						eventually(t, func() bool { return value(cid, device, read.kind) == want })
					}
				}
				if err = e.WriteDeviceModbus(cid, "two", 6, 0, 333); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { return value(sid, "two", "holding") == 333 })
				if value(sid, "one", "holding") != 111 {
					t.Fatal("serial write changed another device")
				}
				if err = e.WriteDeviceModbus(cid, "two", 5, 0, 0); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { return value(sid, "two", "coil") == 0 })
			}
			stopped := make(chan struct{})
			go func() { e.StopAll(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("serial stop did not cancel blocked readers")
			}
		})
	}
}
