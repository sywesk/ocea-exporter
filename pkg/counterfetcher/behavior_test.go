package counterfetcher

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
)

func reading(serial string, value float64) oceaapi.Device {
	return oceaapi.Device{AppareilID: serial, NumeroCompteurAppareil: serial, Fluide: "EauFroide", ValeurIndex: value}
}

func TestCounterSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name      string
		before    []CounterState
		devices   []oceaapi.Device
		want      []CounterState
		wantError bool
	}{
		{name: "initial zero is a reading", devices: []oceaapi.Device{reading("a", 0)}, want: []CounterState{{SerialNumber: "a", Fluid: "EauFroide"}}},
		{name: "existing meter changes", before: []CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 10}}, devices: []oceaapi.Device{reading("a", 11)}, want: []CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 11}}},
		{name: "new meter", before: []CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 10}}, devices: []oceaapi.Device{reading("a", 10), reading("b", 20)}, want: []CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 10}, {SerialNumber: "b", Fluid: "EauFroide", AbsoluteIndex: 20}}},
		{name: "replacement", before: []CounterState{{SerialNumber: "old", Fluid: "EauFroide", AbsoluteIndex: 100}}, devices: []oceaapi.Device{reading("new", 0)}, want: []CounterState{{SerialNumber: "new", Fluid: "EauFroide"}}},
		{name: "duplicate serial is rejected", devices: []oceaapi.Device{reading("a", 1), reading("a", 2)}, wantError: true},
		{name: "missing serial is rejected", devices: []oceaapi.Device{reading("", 1)}, wantError: true},
		{name: "empty snapshot is rejected", wantError: true},
		{name: "missing fluid is rejected", devices: []oceaapi.Device{{AppareilID: "a", NumeroCompteurAppareil: "a", ValeurIndex: 1}}, wantError: true},
		{name: "negative index is rejected", devices: []oceaapi.Device{reading("a", -1)}, wantError: true},
		{name: "nonfinite index is rejected atomically", before: []CounterState{{SerialNumber: "a", AbsoluteIndex: 10}}, devices: []oceaapi.Device{reading("a", 11), reading("b", math.NaN())}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, api, _ := testFetcher(t)
			c.state.CounterStates = append([]CounterState(nil), tc.before...)
			api.today = tc.devices
			api.meters = nil
			for _, device := range tc.devices {
				api.meters = append(api.meters, oceaapi.Meter{ID: device.AppareilID, Serial: device.NumeroCompteurAppareil, PDSID: "pds"})
			}
			err := c.poll()
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			want := tc.want
			if tc.wantError {
				want = tc.before
			}
			if !reflect.DeepEqual(c.state.CounterStates, want) {
				t.Fatalf("got %+v, want %+v", c.state.CounterStates, want)
			}
		})
	}
}

func TestNotificationsRoundWithoutChangingState(t *testing.T) {
	ch := make(chan Notification, 1)
	c := &CounterFetcher{state: state{CounterStates: []CounterState{{SerialNumber: "a", AbsoluteIndex: 1.23456}}}}
	c.RegisterListener(ch)
	c.notifyListeners()
	if got := (<-ch).CounterStates[0].AbsoluteIndex; got != 1.235 {
		t.Fatalf("notification index = %v", got)
	}
	if c.state.CounterStates[0].AbsoluteIndex != 1.23456 {
		t.Fatal("notification changed persisted precision")
	}
}

func TestSlowListenerDoesNotBlockOthers(t *testing.T) {
	blocked := make(chan Notification, 1)
	blocked <- Notification{}
	ready := make(chan Notification, 1)
	c := &CounterFetcher{}
	c.RegisterListener(blocked)
	c.RegisterListener(ready)
	c.notifyListeners()
	select {
	case <-ready:
	default:
		t.Fatal("ready listener missed notification")
	}
}

func TestRejectInvalidPollingIntervals(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		if _, err := New(Settings{StateFilePath: filepath.Join(t.TempDir(), "state.json"), PollInterval: interval}); err == nil {
			t.Errorf("accepted interval %v", interval)
		}
	}
}

func TestStateRoundTripAndReplacement(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nested", "state.json")
	want := state{CounterStates: []CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 1.23456}}}
	for _, value := range []float64{1.23456, 2} {
		want.CounterStates[0].AbsoluteIndex = value
		if err := want.save(file); err != nil {
			t.Fatal(err)
		}
		got, err := loadState(file)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("state = %+v, error = %v", got, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Dir(file))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("directory permissions = %o", info.Mode().Perm())
		}
	}
}

func TestStateLoadFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	if _, err := loadState(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(file); err == nil {
		t.Fatal("accepted corrupt state")
	}
	if _, err := loadState(filepath.Dir(file)); err == nil {
		t.Fatal("accepted directory as state file")
	}
}

func TestNotificationConsumersHaveIndependentSnapshots(t *testing.T) {
	first, second := make(chan Notification, 1), make(chan Notification, 1)
	c := &CounterFetcher{state: state{CounterStates: []CounterState{{SerialNumber: "a", AbsoluteIndex: 1.23456}}}}
	c.RegisterListener(first)
	c.RegisterListener(second)
	c.notifyListeners()
	(<-first).CounterStates[0].AbsoluteIndex = 99
	if got := (<-second).CounterStates[0].AbsoluteIndex; got != 1.235 {
		t.Fatalf("consumer mutation leaked: %v", got)
	}
	if c.state.CounterStates[0].AbsoluteIndex != 1.23456 {
		t.Fatal("consumer changed source state")
	}
}

func TestFailedStateSavePreservesPreviousFileAndCleansTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.json")
	previous := state{CounterStates: []CounterState{{SerialNumber: "a", AbsoluteIndex: 1}}}
	if err := previous.save(file); err != nil {
		t.Fatal(err)
	}
	invalid := state{CounterStates: []CounterState{{SerialNumber: "a", AbsoluteIndex: math.NaN()}}}
	if err := invalid.save(file); err == nil {
		t.Fatal("accepted invalid state")
	}
	got, err := loadState(file)
	if err != nil || !reflect.DeepEqual(got, previous) {
		t.Fatalf("previous snapshot damaged: %+v, %v", got, err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "keep"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := previous.save(blocked); err == nil {
		t.Fatal("expected rename failure")
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".ocea-state-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v, %v", leftovers, err)
	}
}
