package counterfetcher

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
)

type fakeAPI struct {
	today, yesterday                         []oceaapi.Device
	meters                                   []oceaapi.Meter
	err                                      error
	ownerCalls, occupantCalls, residentCalls int
	day                                      time.Time
}

func (f *fakeAPI) GetResident() (oceaapi.Resident, error) {
	f.residentCalls++
	var resident oceaapi.Resident
	json.Unmarshal([]byte(`{"occupations":[{"logementId":"home","typeOccupation":"Proprietaire"}]}`), &resident)
	return resident, f.err
}
func (f *fakeAPI) GetLocal(string) (oceaapi.Local, error) {
	local := oceaapi.Local{FluidesRestitues: []oceaapi.Fluid{{Fluide: "EauFroide"}}}
	local.Local.ID = "home"
	return local, f.err
}
func (f *fakeAPI) GetMeters(string) ([]oceaapi.Meter, error) { return f.meters, f.err }
func (f *fakeAPI) GetDevices(_ string, day time.Time) ([]oceaapi.Device, error) {
	f.ownerCalls++
	if day.Format("2006-01-02") == f.day.Format("2006-01-02") {
		return f.today, f.err
	}
	return f.yesterday, f.err
}
func (f *fakeAPI) GetOccupantDevices(string, time.Time) ([]oceaapi.Device, error) {
	f.occupantCalls++
	return f.today, f.err
}

type memoryStore struct {
	snapshot state
	err      error
	saves    int
}

func (s *memoryStore) Load(string) (state, error) { return s.snapshot, nil }
func (s *memoryStore) Save(_ string, next state) error {
	s.saves++
	if s.err != nil {
		return s.err
	}
	s.snapshot = next
	return nil
}

func testFetcher(t *testing.T) (*CounterFetcher, *fakeAPI, *memoryStore) {
	t.Helper()
	c, err := New(Settings{StateFilePath: filepath.Join(t.TempDir(), "state.json"), PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	api := &fakeAPI{day: day, meters: []oceaapi.Meter{{ID: "a", Serial: "a", PDSID: "pds-a"}}, today: []oceaapi.Device{reading("a", 10)}}
	store := &memoryStore{}
	c.apiClient = api
	c.store = store
	c.now = func() time.Time { return day }
	return c, api, store
}

func TestOwnerBackfillUsesCurrentInventory(t *testing.T) {
	c, api, _ := testFetcher(t)
	api.meters = append(api.meters, oceaapi.Meter{ID: "b", Serial: "b", PDSID: "pds-b"})
	api.yesterday = []oceaapi.Device{reading("a", 9), reading("b", 20), reading("retired", 30)}
	devices, err := c.fetchDevices("home", "Proprietaire")
	if err != nil || !reflect.DeepEqual(devices, []oceaapi.Device{reading("a", 10), reading("b", 20)}) {
		t.Fatalf("wrong backfill: %+v, %v", devices, err)
	}
	api.yesterday = nil
	if _, err := c.fetchDevices("home", "Proprietaire"); err == nil {
		t.Fatal("accepted partial readings")
	}
}

func TestReplacementDoesNotRequireRetiredMeter(t *testing.T) {
	c, api, _ := testFetcher(t)
	c.state.AccountData.Devices = []oceaapi.Device{reading("retired", 100)}
	devices, err := c.fetchDevices("home", "Proprietaire")
	if err != nil || len(devices) != 1 || devices[0].AppareilID != "a" {
		t.Fatalf("replacement blocked: %+v, %v", devices, err)
	}
	if api.ownerCalls != 1 {
		t.Fatal("unnecessary historical request")
	}
}

func TestOccupantDoesNotCallOwnerEndpoints(t *testing.T) {
	c, api, _ := testFetcher(t)
	if _, err := c.fetchDevices("home", "Occupant"); err != nil {
		t.Fatal(err)
	}
	if api.occupantCalls != 1 || api.ownerCalls != 0 {
		t.Fatal("wrong account flow")
	}
}

func TestPollRetriesFailedPersistenceWithoutPublishing(t *testing.T) {
	c, api, store := testFetcher(t)
	ch := make(chan Notification, 2)
	c.RegisterListener(ch)
	store.err = errors.New("disk full")
	if err := c.poll(); err == nil {
		t.Fatal("expected save failure")
	}
	if len(c.state.CounterStates) != 0 || len(ch) != 0 {
		t.Fatal("failed save committed or published state")
	}
	store.err = nil
	if err := c.poll(); err != nil {
		t.Fatal(err)
	}
	if store.saves != 2 || len(ch) != 1 || c.state.CounterStates[0].AbsoluteIndex != 10 {
		t.Fatal("did not retry unchanged readings")
	}
	api.today = []oceaapi.Device{reading("a", 11)}
	if err := c.poll(); err != nil {
		t.Fatal(err)
	}
	if len(ch) != 2 || store.snapshot.CounterStates[0].AbsoluteIndex != 11 {
		t.Fatal("new snapshot not saved and notified")
	}
}

func metricValue(t *testing.T, c *CounterFetcher, name string) float64 {
	t.Helper()
	registry := prometheus.NewRegistry()
	registry.MustRegister(c)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family.Metric[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("missing metric %s", name)
	return 0
}

func TestCollectionHealthAndReadingFreshness(t *testing.T) {
	c, api, _ := testFetcher(t)
	api.today[0].Date = "2026-10-21T00:00:00"
	if err := c.poll(); err != nil {
		t.Fatal(err)
	}
	if metricValue(t, c, "ocea_scrape_success") != 1 {
		t.Fatal("successful poll marked failed")
	}
	lastSuccess := metricValue(t, c, "ocea_last_success_timestamp_seconds")
	if metricValue(t, c, "ocea_metering_reading_timestamp_seconds") != float64(api.day.AddDate(0, 0, -1).Truncate(24*time.Hour).Unix()) {
		t.Fatal("reading timestamp replaced by polling time")
	}
	api.err = errors.New("offline")
	if err := c.poll(); err == nil {
		t.Fatal("expected API failure")
	}
	if metricValue(t, c, "ocea_scrape_success") != 0 || metricValue(t, c, "ocea_last_success_timestamp_seconds") != lastSuccess {
		t.Fatal("failure hid stale data")
	}
}

func TestWorkerStopsAndRejectsSecondStart(t *testing.T) {
	c, _, _ := testFetcher(t)
	updates := make(chan Notification, 1)
	c.RegisterListener(updates)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); err == nil {
		t.Fatal("started duplicate worker")
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("initial poll did not run")
	}
	cancel()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestRetiredMetersAndFluidsDisappearFromMetrics(t *testing.T) {
	c, api, _ := testFetcher(t)
	api.today[0].Date = "2026-10-22T00:00:00"
	if err := c.poll(); err != nil {
		t.Fatal(err)
	}
	api.meters = []oceaapi.Meter{{ID: "b", Serial: "b", PDSID: "pds-b"}}
	api.today = []oceaapi.Device{reading("b", 2)}
	api.today[0].Fluide = oceaapi.FluidHotWater
	if err := c.poll(); err != nil {
		t.Fatal(err)
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(c)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "serial" && label.GetValue() == "a" || label.GetName() == "fluid" && label.GetValue() == string(oceaapi.FluidColdWater) {
					t.Fatal("retired series is still exposed")
				}
			}
		}
	}
}

func TestConcurrentListenerRegistrationAndMetricsScrapes(t *testing.T) {
	c, _, _ := testFetcher(t)
	registry := prometheus.NewRegistry()
	registry.MustRegister(c)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			c.RegisterListener(make(chan Notification, 1))
			if _, err := registry.Gather(); err != nil {
				t.Error(err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			if err := c.poll(); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
}

func TestFailedRefreshDoesNotMutatePreviousSnapshot(t *testing.T) {
	c, api, store := testFetcher(t)
	if err := c.poll(); err != nil {
		t.Fatal(err)
	}
	before := c.state
	api.meters = append(api.meters, oceaapi.Meter{ID: "b", Serial: "b", PDSID: "pds-b"})
	api.today = []oceaapi.Device{reading("a", 11)}
	if err := c.poll(); err == nil {
		t.Fatal("accepted incomplete snapshot")
	}
	if !reflect.DeepEqual(c.state, before) || store.saves != 1 {
		t.Fatal("failed poll changed previous snapshot")
	}
}

func TestWorkerRecoversOnNextPollAfterPanic(t *testing.T) {
	c, api, _ := testFetcher(t)
	c.now = func() time.Time { panic("test clock failure") }
	if err := c.safePoll(); err == nil {
		t.Fatal("panic was not reported")
	}
	c.now = func() time.Time { return api.day }
	if err := c.safePoll(); err != nil {
		t.Fatalf("poll did not recover: %v", err)
	}
}
