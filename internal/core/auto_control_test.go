package core

import (
	"encoding/hex"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func autoFixture(t *testing.T, points []Point) (*Engine, Channel, Channel) {
	t.Helper()
	e, client, server := controlLoopback(t)
	if err := e.SetDevicePoints(client.ID, client.Devices[0].ID, points); err != nil {
		t.Fatal(err)
	}
	if err := e.SetDevicePoints(server.ID, server.Devices[0].ID, points); err != nil {
		t.Fatal(err)
	}
	return e, client, server
}
func prepareYK(t *testing.T, e *Engine, c Channel) AutoControlSnapshot {
	t.Helper()
	v, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yk", AutoControlSeed{})
	if err != nil {
		t.Fatal(err)
	}
	o := v.Options
	o.IntervalMS = 0
	o.TimeoutMS = 1000
	v, err = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yk", o)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func autoWait(t *testing.T, e *Engine, c Channel, mode, status string) AutoControlSnapshot {
	t.Helper()
	var v AutoControlSnapshot
	eventually(t, func() bool { v, _ = e.GetAutoControl(c.ID, c.Devices[0].ID, mode); return v.Status == status })
	return v
}
func autoCommands(e *Engine, id string) [][]byte {
	out := [][]byte{}
	for _, m := range e.Snapshot().Messages {
		if m.ChannelID != id || m.Direction != "TX" {
			continue
		}
		b, _ := hex.DecodeString(m.Hex)
		if len(b) >= 16 && b[2]&1 == 0 && controlKind(b[6]) != "" {
			out = append(out, b[6:])
		}
	}
	return out
}

func TestAutoYKDefaultsBulkAndOperationOrdering(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yk", Address: 101, Name: "one"}, {Kind: "yk", Address: 100, Name: "two"}})
	v := prepareYK(t, e, c)
	if len(v.Rows) != 2 || v.Rows[0].Address != 101 || v.Rows[0].Action != "合闸后分闸" || !v.Rows[0].Enabled || !v.Options.AutoPause {
		t.Fatalf("wrong original defaults: %+v", v)
	}
	if _, err := e.ApplyAutoBulk(c.ID, c.Devices[0].ID, AutoControlBulk{ChangeCount: true, Count: 2, ChangeAction: true, Action: "分闸后合闸"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yk", "completed")
	if v.Total != 8 || v.Success != 8 || v.Failure != 0 {
		t.Fatalf("wrong counters: %+v", v)
	}
	commands := autoCommands(e, c.ID)
	if len(commands) != 16 {
		t.Fatalf("expected select+execute per operation, got %d", len(commands))
	}
	for i, a := range commands {
		wantAddress := uint32(101)
		if i >= 8 {
			wantAddress = 100
		}
		if controlID(a).address != wantAddress || a[0] != 45 {
			t.Fatalf("wrong address/order: %x", a)
		}
		on := (i/2)%2 == 1
		if a[9]&1 != 0 != on {
			t.Fatalf("wrong open/close sequence: %x", a)
		}
		if (a[9]&0x80 != 0) != (i%2 == 0) {
			t.Fatalf("select/execute order: %x", a)
		}
	}
	d, _ := e.Device(s.ID, s.Devices[0].ID)
	for _, p := range d.Points {
		if p.Value != 1 {
			t.Fatal("final close command was not executed")
		}
	}
	// Snapshot mutations cannot change a live job.
	v.Rows[0].Count = 999
	again, _ := e.GetAutoControl(c.ID, c.Devices[0].ID, "yk")
	if again.Rows[0].Count != 2 {
		t.Fatal("snapshot aliases engine state")
	}
}
func TestAutoYKDirectDoubleSkipsDisabledRows(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yk", Address: 100}, {Kind: "yk", Address: 101}})
	v := prepareYK(t, e, c)
	no := false
	action := "合闸"
	if _, err := e.UpdateAutoRows(c.ID, c.Devices[0].ID, "yk", []AutoControlRowEdit{{Index: 0, Enabled: &no}, {Index: 1, Action: &action}}); err != nil {
		t.Fatal(err)
	}
	o := v.Options
	o.Direct = true
	o.Double = true
	if _, err := e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yk", o); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yk", "completed")
	commands := autoCommands(e, c.ID)
	if len(commands) != 1 || commands[0][0] != 46 || commands[0][9] != 2 || controlID(commands[0]).address != 101 || v.Total != 1 {
		t.Fatalf("wrong direct double commands: %+v %x", v, commands)
	}
}
func TestAutoYKFailurePauseContinuesAtNextOperation(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yk", Address: 100}, {Kind: "yk", Address: 101}})
	prepareYK(t, e, c)
	if err := e.SetPoints(s.ID, []Point{{Kind: "yk", Address: 101}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyAutoBulk(c.ID, c.Devices[0].ID, AutoControlBulk{ChangeAction: true, Action: "合闸"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v := autoWait(t, e, c, "yk", "paused")
	if v.Total != 1 || v.Failure != 1 || v.Success != 0 || len(v.Errors) != 1 {
		t.Fatalf("not paused at failure: %+v", v)
	}
	if v.Rows[0].Selection != "第1次合闸选择拒绝" {
		t.Fatal("wrong original rejection caption: " + v.Rows[0].Selection)
	}
	before := len(autoCommands(e, c.ID))
	time.Sleep(30 * time.Millisecond)
	if len(autoCommands(e, c.ID)) != before {
		t.Fatal("commands sent while paused")
	}
	if _, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yk", AutoControlSeed{}); err != nil {
		t.Fatal(err)
	}
	again, _ := e.GetAutoControl(c.ID, c.Devices[0].ID, "yk")
	if again.ID != v.ID || again.Status != "paused" || again.Total != 1 {
		t.Fatal("reopening reset paused task")
	}
	if _, err := e.ResumeAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yk", "completed")
	if v.Total != 2 || v.Success != 1 || v.Failure != 1 || v.Rows[1].Execution != "第1次合闸执行成功" {
		t.Fatalf("resume restarted or skipped a row: %+v", v)
	}
}
func TestAutoYKCyclePauseStopAndExclusiveOwnership(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yk", Address: 100}})
	v := prepareYK(t, e, c)
	o := v.Options
	o.Cycle = true
	o.IntervalMS = 20
	_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yk", o)
	_, _ = e.ApplyAutoBulk(c.ID, c.Devices[0].ID, AutoControlBulk{ChangeAction: true, Action: "合闸"})
	if _, err := e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ = e.GetAutoControl(c.ID, c.Devices[0].ID, "yk"); return v.Success >= 3 })
	if _, err := e.Command104Device(c.ID, c.Devices[0].ID, Control104Request{Type: 45, Address: 100, Value: 0, Operation: "select", TimeoutMS: 1000}); err == nil {
		t.Fatal("manual command interleaved with auto sequence")
	}
	if _, err := e.PauseAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	before := len(autoCommands(e, c.ID))
	time.Sleep(50 * time.Millisecond)
	if len(autoCommands(e, c.ID)) != before {
		t.Fatal("pause failed")
	}
	if _, err := e.StopAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	autoWait(t, e, c, "yk", "stopped")
	time.Sleep(30 * time.Millisecond)
	if len(autoCommands(e, c.ID)) != before {
		t.Fatal("commands sent after stop")
	}
	// A completed stop releases the channel for manual operation again.
	if _, err := e.Command104Device(c.ID, c.Devices[0].ID, Control104Request{Type: 45, Address: 100, Value: 0, Operation: "select", TimeoutMS: 1000}); err != nil {
		t.Fatal(err)
	}
}
func TestAutoYKFeedbackChecksRealReportsAndInverseLink(t *testing.T) {
	points := []Point{{Kind: "yk", Address: 100, Feedback: "1"}, {Kind: "yk", Address: 101, Feedback: "!2"}, {Kind: "yx", Address: 1, Name: "positive", Value: 0}, {Kind: "yx", Address: 2, Name: "inverse", Value: 0}}
	e, c, s := autoFixture(t, points)
	v := prepareYK(t, e, c)
	o := v.Options
	o.CheckFeedback = true
	_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yk", o)
	_, _ = e.ApplyAutoBulk(c.ID, c.Devices[0].ID, AutoControlBulk{ChangeAction: true, Action: "合闸"})
	publishDone := make(chan struct{})
	go func() {
		defer close(publishDone)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			d, _ := e.Device(s.ID, s.Devices[0].ID)
			if d.Points[0].Value == 1 {
				_ = e.PatchDevicePoint(s.ID, s.Devices[0].ID, &PointKey{Kind: "yx", Address: 1}, Point{Value: 1}, []string{"value"})
				_ = e.PublishDevice(s.ID, s.Devices[0].ID)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if _, err := e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yk", "completed")
	<-publishDone
	if v.Success != 2 || v.Failure != 0 || v.Rows[0].FeedbackResult != "变位成功" || v.Rows[1].FeedbackResult != "变位成功" {
		t.Fatalf("feedback not based on real YX: %+v", v)
	}
}
func TestAutoYKFeedbackFailureIsNotExecutionSuccess(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yk", Address: 100, Feedback: "1"}, {Kind: "yx", Address: 1, Name: "position", Value: 0}})
	v := prepareYK(t, e, c)
	o := v.Options
	o.CheckFeedback = true
	o.TimeoutMS = 100
	_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yk", o)
	_, _ = e.ApplyAutoBulk(c.ID, c.Devices[0].ID, AutoControlBulk{ChangeAction: true, Action: "合闸"})
	if _, err := e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yk", "completed")
	if v.Success != 0 || v.Failure != 1 || v.Rows[0].FeedbackResult != "变位失败" {
		t.Fatalf("stale YX was reported as success: %+v", v)
	}
}
func TestAutoYTOriginalAddressOrderingAndValues(t *testing.T) {
	addresses, err := ParseAutoAddresses("5,1-2,9-10,5")
	if err != nil || !reflect.DeepEqual(addresses, []uint32{5, 5, 1, 2, 9, 10}) {
		t.Fatalf("source order mismatch: %v %v", addresses, err)
	}
	for _, input := range []string{"", "1,", "5-2", "0-10000", "16777216", "1--2"} {
		if _, err := ParseAutoAddresses(input); err == nil {
			t.Fatalf("invalid addresses accepted: %q", input)
		}
	}
	for _, typ := range []byte{48, 49, 50} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			e, c, s := autoFixture(t, []Point{{Kind: "yt", Address: 1}, {Kind: "yt", Address: 2}})
			v, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yt", AutoControlSeed{Addresses: "1-2", Value: "10H", ValueType: typ})
			if err != nil {
				t.Fatal(err)
			}
			if v.Options.IntervalMS != 200 || v.Rows[0].Executed != "否" {
				t.Fatal("wrong original YT defaults")
			}
			o := v.Options
			o.IntervalMS = 0
			o.TimeoutMS = 1000
			_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yt", o)
			value := "-5"
			if typ == 50 {
				value = "1.5"
			}
			if _, err = e.UpdateAutoRows(c.ID, c.Devices[0].ID, "yt", []AutoControlRowEdit{{Index: 1, Value: &value}}); err != nil {
				t.Fatal(err)
			}
			if _, err = e.StartAutoControl(c.ID, c.Devices[0].ID, "yt"); err != nil {
				t.Fatal(err)
			}
			v = autoWait(t, e, c, "yt", "completed")
			if v.Success != 2 || v.Rows[0].Executed != "是" || v.Rows[1].Executed != "是" {
				t.Fatalf("YT execution status: %+v", v)
			}
			d, _ := e.Device(s.ID, s.Devices[0].ID)
			want := float64(-5)
			if typ == 50 {
				want = 1.5
			}
			if d.Points[0].Value != 16 || d.Points[1].Value != want {
				t.Fatalf("YT did not reach the device: %+v", d.Points)
			}
		})
	}
}
func TestAutoValidationIsAtomicAndDoesNotSend(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yt", Address: 1}})
	_, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yt", AutoControlSeed{Addresses: "1", Value: "invalid", ValueType: 49})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.StartAutoControl(c.ID, c.Devices[0].ID, "yt"); err == nil {
		t.Fatal("invalid value started a job")
	}
	if len(autoCommands(e, c.ID)) != 0 {
		t.Fatal("invalid configuration sent a command")
	}
	e, c, _ = autoFixture(t, []Point{{Kind: "yk", Address: 1}, {Kind: "yk", Address: 2}})
	prepareYK(t, e, c)
	count, bad := 3, 0
	if _, err = e.UpdateAutoRows(c.ID, c.Devices[0].ID, "yk", []AutoControlRowEdit{{Index: 0, Count: &count}, {Index: 1, Count: &bad}}); err == nil {
		t.Fatal("invalid bulk edit accepted")
	}
	v, _ := e.GetAutoControl(c.ID, c.Devices[0].ID, "yk")
	if v.Rows[0].Count != 1 {
		t.Fatal("partially applied an invalid edit")
	}
	if _, err = e.ApplyAutoBulk(c.ID, c.Devices[0].ID, AutoControlBulk{ChangeEnabled: true, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err == nil {
		t.Fatal("empty enabled set started")
	}
}
func TestAutoYTStopDuringIntervalPreventsNextRow(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yt", Address: 1}, {Kind: "yt", Address: 2}})
	v, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yt", AutoControlSeed{Addresses: "1,2", Value: "42", ValueType: 50})
	if err != nil {
		t.Fatal(err)
	}
	o := v.Options
	o.IntervalMS = 1000
	_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yt", o)
	if _, err = e.StartAutoControl(c.ID, c.Devices[0].ID, "yt"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ = e.GetAutoControl(c.ID, c.Devices[0].ID, "yt"); return v.Success == 1 })
	// Reopening a hidden, running window must not rebuild its edited table.
	reopened, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yt", AutoControlSeed{Addresses: "999", Value: "0", ValueType: 48})
	if err != nil || reopened.ID != v.ID || len(reopened.Rows) != 2 {
		t.Fatal("reopening reset an active job")
	}
	_, _ = e.StopAutoControl(c.ID, c.Devices[0].ID, "yt")
	v = autoWait(t, e, c, "yt", "stopped")
	d, _ := e.Device(s.ID, s.Devices[0].ID)
	if d.Points[0].Value != 42 || d.Points[1].Value != 0 || v.Rows[1].Executed != "否" {
		t.Fatal("a later row executed after stop")
	}
}
func TestAutoStopDuringSelectionNeverSendsExecute(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan []byte, 10)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			b, err := Read104(conn)
			if err != nil {
				return
			}
			if b[2] == StartAct {
				_, _ = conn.Write(UFrame(StartCon))
			} else if b[2]&1 == 0 {
				received <- b
				_, _ = conn.Write(SFrame((uint16(b[2])|uint16(b[3])<<8)/2 + 1))
			}
		}
	}()
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "held selection", Protocol: "iec104", Role: "client", Transport: "tcp-client", Address: listener.Addr().String(), CommonAddress: 1, Points: []Point{{Kind: "yk", Address: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	c := e.Snapshot().Project.Channels[0]
	prepareYK(t, e, c)
	eventually(t, func() bool { _, err = e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); return err == nil })
	select {
	case b := <-received:
		if b[15]&0x80 == 0 {
			t.Fatal("first command was not select")
		}
	case <-time.After(time.Second):
		t.Fatal("no select command")
	}
	_, _ = e.StopAutoControl(c.ID, c.Devices[0].ID, "yk")
	autoWait(t, e, c, "yk", "stopped")
	select {
	case b := <-received:
		t.Fatalf("sent another command after stop: %x", b)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err = e.Command104Device(c.ID, c.Devices[0].ID, Control104Request{Type: 45, Address: 100, Value: 1, Operation: "select", TimeoutMS: 100}); err == nil || !strings.Contains(err.Error(), "重连") {
		t.Fatal("cancelled in-flight request did not quarantine late replies")
	}
}

func TestAutoStopAllAndProjectReplacementInvalidateJobs(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yk", Address: 100}})
	v := prepareYK(t, e, c)
	o := v.Options
	o.Cycle = true
	o.IntervalMS = 50
	_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yk", o)
	if _, err := e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ = e.GetAutoControl(c.ID, c.Devices[0].ID, "yk"); return v.Success >= 1 })
	e.StopAll()
	autoWait(t, e, c, "yk", "stopped")
	before := len(autoCommands(e, c.ID))
	time.Sleep(60 * time.Millisecond)
	if len(autoCommands(e, c.ID)) != before {
		t.Fatal("StopAll left an automatic worker running")
	}
	if err := e.ReplaceProjectAndStop(Project{Version: 2, Name: "new", Channels: []Channel{}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.GetAutoControl(c.ID, c.Devices[0].ID, "yk"); err == nil {
		t.Fatal("old task survived project replacement")
	}
}
func TestAutoYTTypeInheritsLiveSourceAndStaleEditsAreRejected(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yt", Address: 1}, {Kind: "yt", Address: 2}})
	v, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yt", AutoControlSeed{Addresses: "1,2", Value: "1", ValueType: 48})
	if err != nil {
		t.Fatal(err)
	}
	o := v.Options
	o.IntervalMS = 100
	_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yt", o)
	if _, err = e.StartAutoControl(c.ID, c.Devices[0].ID, "yt"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ = e.GetAutoControl(c.ID, c.Devices[0].ID, "yt"); return v.Success == 1 })
	if err = e.SetAutoValueType(c.ID, c.Devices[0].ID, 50); err != nil {
		t.Fatal(err)
	}
	if _, err = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yt", o); err != nil {
		t.Fatal(err)
	}
	autoWait(t, e, c, "yt", "completed")
	commands := autoCommands(e, c.ID)
	if len(commands) != 2 || commands[0][0] != 48 || commands[1][0] != 50 {
		t.Fatalf("type did not follow original source control: %x", commands)
	}
	oldGeneration := v.Generation
	_, err = e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yt", AutoControlSeed{Addresses: "2", Value: "2", ValueType: 50})
	if err != nil {
		t.Fatal(err)
	}
	value := "99"
	if _, err = e.UpdateAutoRows(c.ID, c.Devices[0].ID, "yt", []AutoControlRowEdit{{Index: 0, Generation: oldGeneration, Value: &value}}); err == nil {
		t.Fatal("late editor request overwrote a new operation list")
	}
}
