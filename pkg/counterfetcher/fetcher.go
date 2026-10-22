package counterfetcher

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
	"github.com/sywesk/ocea-exporter/pkg/oceaauth"
	"go.uber.org/zap"
)

// meterAPI exposes only the portal operations used by the poller.
type meterAPI interface {
	GetResident() (oceaapi.Resident, error)
	GetLocal(string) (oceaapi.Local, error)
	GetMeters(string) ([]oceaapi.Meter, error)
	GetDevices(string, time.Time) ([]oceaapi.Device, error)
	GetOccupantDevices(string, time.Time) ([]oceaapi.Device, error)
}

// CounterFetcher publishes complete, persisted snapshots of the current meters.
// Its state is owned by a single worker; listeners may be registered concurrently.
type CounterFetcher struct {
	settings  Settings
	state     state
	apiClient meterAPI
	store     stateStore
	now       func() time.Time
	metrics   *fetcherMetrics
	mu        sync.Mutex
	listeners []chan<- Notification
	started   bool
	done      chan struct{}
}

type Settings struct {
	StateFilePath string
	Username      string
	Password      string
	PollInterval  time.Duration
}

func New(settings Settings) (*CounterFetcher, error) {
	if settings.StateFilePath == "" {
		return nil, fmt.Errorf("empty state file location")
	}
	if settings.PollInterval <= 0 {
		return nil, fmt.Errorf("poll interval must be positive")
	}
	return &CounterFetcher{settings: settings, store: fileStateStore{}, now: time.Now, metrics: newFetcherMetrics(), done: make(chan struct{})}, nil
}

func (c *CounterFetcher) RegisterListener(listener chan<- Notification) {
	if listener == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listeners = append(c.listeners, listener)
}

func (c *CounterFetcher) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return fmt.Errorf("fetcher already started")
	}
	snapshot, err := c.store.Load(c.settings.StateFilePath)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}
	c.state = snapshot
	if c.apiClient == nil {
		c.apiClient = oceaapi.NewClient(oceaauth.NewTokenProvider(c.settings.Username, c.settings.Password)).WithContext(ctx)
	}
	c.started = true
	go c.worker(ctx)
	return nil
}

func (c *CounterFetcher) Done() <-chan struct{} { return c.done }

func (c *CounterFetcher) worker(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(c.settings.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.safePoll(); err != nil {
			zap.L().Error("failed to refresh meters; will retry", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Recover at the iteration boundary, so unexpected parser failures do not cause
// recursive restarts, tight loops, or abandoned tickers.
func (c *CounterFetcher) safePoll() (err error) {
	defer func() {
		if p := recover(); p != nil {
			c.metrics.recordFailure()
			err = fmt.Errorf("meter poll panicked: %v", p)
		}
	}()
	return c.poll()
}

func (c *CounterFetcher) poll() (err error) {
	defer func() {
		if err != nil {
			c.metrics.recordFailure()
		}
	}()
	account := c.state.AccountData
	if account.Local.Local.ID == "" || len(account.Resident.Occupations) == 0 {
		account, err = c.fetchInitialState()
		if err != nil {
			return err
		}
	}
	if err := c.fetchCounters(account); err != nil {
		return err
	}
	c.metrics.recordSuccess(c.state, c.now())
	c.notifyListeners()
	return nil
}

type Notification struct{ CounterStates []CounterState }

func (c *CounterFetcher) notifyListeners() {
	c.mu.Lock()
	listeners := append([]chan<- Notification(nil), c.listeners...)
	c.mu.Unlock()
	for _, listener := range listeners {
		// Each consumer owns its snapshot; mutations cannot affect another consumer.
		states := append([]CounterState(nil), c.state.CounterStates...)
		for i := range states {
			states[i].AbsoluteIndex = round3(states[i].AbsoluteIndex)
		}
		select {
		case listener <- Notification{CounterStates: states}:
		default:
			zap.L().Warn("listener channel full; skipping notification")
		}
	}
}

func (c *CounterFetcher) fetchCounters(account rawAccountData) error {
	if len(account.Resident.Occupations) == 0 {
		return fmt.Errorf("no occupation found")
	}
	devices, err := c.fetchDevices(account.Local.Local.ID, account.Resident.Occupations[0].TypeOccupation)
	if err != nil {
		return fmt.Errorf("fetching devices: %w", err)
	}
	counters, err := buildCounterStates(devices)
	if err != nil {
		return err
	}
	account.Devices = devices
	next := state{AccountData: account, CounterStates: counters}
	if !reflect.DeepEqual(c.state, next) {
		if err := c.store.Save(c.settings.StateFilePath, next); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		c.state = next
	}
	return nil
}

func (c *CounterFetcher) fetchInitialState() (rawAccountData, error) {
	resident, err := c.apiClient.GetResident()
	if err != nil {
		return rawAccountData{}, fmt.Errorf("getting resident: %w", err)
	}
	if len(resident.Occupations) == 0 {
		return rawAccountData{}, fmt.Errorf("no occupation found")
	}
	if len(resident.Occupations) > 1 {
		zap.L().Warn("multiple occupations found; using the first", zap.Int("count", len(resident.Occupations)))
	}
	localID := resident.Occupations[0].LogementID
	if localID == "" {
		return rawAccountData{}, fmt.Errorf("occupation has no local ID")
	}
	local, err := c.apiClient.GetLocal(localID)
	if err != nil {
		return rawAccountData{}, fmt.Errorf("getting local: %w", err)
	}
	if local.Local.ID != localID {
		return rawAccountData{}, fmt.Errorf("local ID does not match occupation")
	}
	if len(local.FluidesRestitues) == 0 {
		return rawAccountData{}, fmt.Errorf("no fluid found for local %s", localID)
	}
	return rawAccountData{Resident: resident, Local: local}, nil
}

func (c *CounterFetcher) fetchDevices(localID, occupation string) ([]oceaapi.Device, error) {
	now := c.now()
	if occupation == "Occupant" {
		return c.apiClient.GetOccupantDevices(localID, now)
	}
	if occupation != "Proprietaire" {
		return nil, fmt.Errorf("unsupported occupation %q", occupation)
	}
	meters, err := c.apiClient.GetMeters(localID)
	if err != nil {
		return nil, fmt.Errorf("getting current inventory: %w", err)
	}
	if len(meters) == 0 {
		return nil, fmt.Errorf("no current meters")
	}
	today, err := c.apiClient.GetDevices(localID, now)
	if err != nil {
		return nil, fmt.Errorf("getting today's readings: %w", err)
	}
	if devices, err := currentReadings(today, meters); err == nil {
		return devices, nil
	}
	yesterday, err := c.apiClient.GetDevices(localID, now.AddDate(0, 0, -1))
	if err != nil {
		return nil, fmt.Errorf("getting yesterday's readings: %w", err)
	}
	return currentReadings(mergeDevices(today, yesterday), meters)
}

// The current inventory, rather than the old state file, is authoritative.
// A missing current meter fails the snapshot; retired meters are discarded.
func currentReadings(readings []oceaapi.Device, meters []oceaapi.Meter) ([]oceaapi.Device, error) {
	byID := make(map[string]oceaapi.Device, len(readings))
	for _, device := range readings {
		if _, exists := byID[device.AppareilID]; exists {
			return nil, fmt.Errorf("duplicate reading for meter %s", device.AppareilID)
		}
		byID[device.AppareilID] = device
	}
	devices := make([]oceaapi.Device, 0, len(meters))
	seen := make(map[string]bool, len(meters))
	for _, meter := range meters {
		if meter.ID == "" || seen[meter.ID] {
			return nil, fmt.Errorf("invalid or duplicate meter ID %q", meter.ID)
		}
		seen[meter.ID] = true
		device, ok := byID[meter.ID]
		if !ok {
			return nil, fmt.Errorf("no reading for current meter %s", meter.ID)
		}
		if meter.Serial != "" && meter.Serial != device.NumeroCompteurAppareil {
			return nil, fmt.Errorf("serial mismatch for meter %s", meter.ID)
		}
		devices = append(devices, device)
	}
	return devices, nil
}

// Keep today's readings and backfill missing meters from yesterday.
func mergeDevices(today, yesterday []oceaapi.Device) []oceaapi.Device {
	merged := append([]oceaapi.Device(nil), today...)
	ids := make(map[string]bool, len(today))
	for _, device := range today {
		ids[device.AppareilID] = true
	}
	for _, device := range yesterday {
		if !ids[device.AppareilID] {
			merged = append(merged, device)
			ids[device.AppareilID] = true
		}
	}
	return merged
}

func buildCounterStates(devices []oceaapi.Device) ([]CounterState, error) {
	if len(devices) == 0 {
		return nil, fmt.Errorf("empty meter snapshot")
	}
	states := make([]CounterState, 0, len(devices))
	serials := make(map[string]bool, len(devices))
	for _, device := range devices {
		serial := device.NumeroCompteurAppareil
		if serial == "" || serials[serial] {
			return nil, fmt.Errorf("missing or duplicate meter serial %q", serial)
		}
		if device.Fluide == "" {
			return nil, fmt.Errorf("missing fluid for meter %s", serial)
		}
		if math.IsNaN(device.ValeurIndex) || math.IsInf(device.ValeurIndex, 0) || device.ValeurIndex < 0 {
			return nil, fmt.Errorf("invalid index for meter %s", serial)
		}
		serials[serial] = true
		states = append(states, CounterState{Fluid: device.Fluide, SerialNumber: serial, AbsoluteIndex: device.ValeurIndex, ReadingDate: device.Date})
	}
	return states, nil
}

func (c *CounterFetcher) Describe(ch chan<- *prometheus.Desc) { c.metrics.describe(ch) }
func (c *CounterFetcher) Collect(ch chan<- prometheus.Metric) { c.metrics.collect(ch) }
