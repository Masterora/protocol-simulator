package tui

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

func TestConnectionLabelsFollowTransportIndependentlyOfStationRole(t *testing.T) {
	m := channelEditorModel()
	e := m.configurationEditor
	for _, transport := range []string{"tcp-client", "tcp-server", "serial"} {
		e.setChoice(configField(e, "transport"), transport)
		for _, role := range []string{"server", "client"} {
			e.setChoice(configField(e, "role"), role)
			label := e.fieldLabel(e.fields[configField(e, "address")])
			if label != addressLabel(transport) {
				t.Fatal(transport, role, label)
			}
			if e.value("address") != "127.0.0.1:2404" {
				t.Fatal("label switch changed endpoint")
			}
			if text := strings.Join(m.configurationBody(120, 30), "\n"); !strings.Contains(text, label) {
				t.Fatal(text)
			}
			m.view = 1
			m.detail.View.Channels = []core.InspectChannel{{ID: "c", Role: role, Transport: transport, Address: "127.0.0.1:2404"}}
			if m.channelAddressTitle() != label {
				t.Fatal(m.channelAddressTitle(), label)
			}
			text := strings.Join(formatDetailLines(m.detail.View.Channels[0]), "\n")
			if !strings.Contains(text, label) || !strings.Contains(text, translated(transport, transportLabels)) {
				t.Fatal(text)
			}
		}
	}
	m.detail.View.Channels = []core.InspectChannel{{Transport: "tcp-client", Address: "192.0.2.1:2404"}, {Transport: "tcp-server", Address: "127.0.0.1:2404"}}
	if m.channelAddressTitle() != "通信地址" || !strings.Contains(m.rows()[0].text, "目标地址：192.0.2.1:2404") {
		t.Fatal(m.rows())
	}
	if !strings.Contains(strings.Join(pointDetailLines("modbus-tcp", core.Point{Address: 12}, nil, nil, false), "\n"), "点位：12") {
		t.Fatal("point number label missing")
	}
}

func TestOlderEngineTransportMetadataIsCachedAndRefreshedByRevision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix observer")
	}
	dir, err := os.MkdirTemp("", "prs-meta-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	engine := core.NewEngine()
	server, err := observe.Start(engine, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	calls := 0
	revision := "first"
	transport := "tcp-server"
	server.SetConfigurationHandler(func(_ context.Context, req projectfile.Request) (projectfile.View, projectfile.Result, error) {
		calls++
		if req.Action != "read" || req.Resource != "channel" {
			t.Error(req)
		}
		return projectfile.View{Revision: revision, Value: projectfile.Fields(map[string]any{"transport": transport, "localAddress": "127.0.0.1:0"})}, projectfile.Result{}, nil
	}, nil, func() string { return revision })
	started := time.Now()
	cache := channelMetadataCache{}
	makeUpdate := func() update {
		return update{socket: server.Socket(), detail: observe.Info{Started: started, ConfigurationOperations: true, ConfigurationRevision: revision, View: core.Inspection{Channels: []core.InspectChannel{{ID: "c", Role: "client"}}}}}
	}
	for i := 0; i < 2; i++ {
		u := makeUpdate()
		cache.enrich(context.Background(), &u)
		if u.detail.View.Channels[0].Transport != "tcp-server" {
			t.Fatal(u)
		}
	}
	if calls != 1 {
		t.Fatal("configuration read on each refresh", calls)
	}
	revision = "second"
	transport = "tcp-client"
	u := makeUpdate()
	cache.enrich(context.Background(), &u)
	if calls != 2 || u.detail.View.Channels[0].Transport != "tcp-client" {
		t.Fatal(calls, u)
	}
	u = makeUpdate()
	u.detail.View.Channels[0].Transport = "serial"
	cache.enrich(context.Background(), &u)
	if calls != 2 || u.detail.View.Channels[0].Transport != "serial" {
		t.Fatal("new engine metadata overridden")
	}
}
