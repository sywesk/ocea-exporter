package counterfetcher

import (
	"fmt"
	"time"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
	"github.com/sywesk/ocea-exporter/pkg/oceaauth"
	"go.uber.org/zap"
)

/*
CounterFetcher is the abstraction that will maintain up-to-date counter values.
*/
type CounterFetcher struct {
	settings  Settings
	state     state
	healthy   bool // Indicates if the last refresh of the counters was successful
	ready     bool // Indicates if the counters are ready
	apiClient oceaapi.APIClient
	listeners []chan<- Notification
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

	return &CounterFetcher{
		settings: settings,
	}, nil
}

func (c *CounterFetcher) RegisterListener(listener chan<- Notification) {
	c.listeners = append(c.listeners, listener)
}

func (c *CounterFetcher) Start() error {
	var err error

	c.state, err = loadState(c.settings.StateFilePath)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	tokenProvider := oceaauth.NewTokenProvider(c.settings.Username, c.settings.Password)
	c.apiClient = oceaapi.NewClient(tokenProvider)

	go c.worker()
	return nil
}

func (c *CounterFetcher) worker() {
	zap.L().Info("fetch worker started")

	defer func() {
		if err := recover(); err != nil {
			zap.L().Error("fetch worker crashed", zap.Any("panic_error", err))
			c.worker()
		}
	}()

	t := time.NewTicker(c.settings.PollInterval)

	for {
		// If the state is empty, then we need to fetch everything first.
		if c.state.AccountData.Resident.NomClient == "" {
			err := c.fetchInitialState()
			if err != nil {
				zap.L().Error("failed to fetch initial state, will retry next time", zap.Error(err))
				<-t.C
				continue
			}
		}

		err := c.fetchCounters()
		if err != nil {
			zap.L().Error("failed to fetch counters, will retry next time", zap.Error(err))
			c.healthy = false
		} else {
			c.healthy = true
			c.ready = true

			c.notifyListeners()
			c.updateCounterMetrics()
		}

		<-t.C
	}
}

type Notification struct {
	CounterStates []CounterState
}

func (c *CounterFetcher) notifyListeners() {
	var states []CounterState

	for _, state := range c.state.CounterStates {
		clonedState := state.Clone()
		clonedState.AbsoluteIndex = round3(clonedState.AbsoluteIndex)
		states = append(states, state.Clone())
	}

	notif := Notification{
		CounterStates: states,
	}

	for _, listener := range c.listeners {
		select {
		case listener <- notif:
			continue
		default:
			zap.L().Warn("failed to notify a listener: channel blocked")
		}
	}
}

func (c *CounterFetcher) updateCounterMetrics() {
	localID := c.state.AccountData.Local.Local.ID

	for _, state := range c.state.CounterStates {
		index.WithLabelValues(state.Fluid, localID).Set(round3(state.AbsoluteIndex))
	}
}

func (c *CounterFetcher) fetchCounters() error {
	devices, err := c.fetchDevices(c.state.AccountData.Local.Local.ID, len(c.state.AccountData.Local.FluidesRestitues))
	if err != nil {
		return fmt.Errorf("fetching devices: %v", err)
	}
	c.state.AccountData.Devices = devices

	countersUpdated, err := c.updateCounters(c.state.AccountData.Devices)
	if err != nil {
		return fmt.Errorf("updating counters: %w", err)
	}

	if countersUpdated {
		err = c.state.save(c.settings.StateFilePath)
		if err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
	} else {
		zap.L().Info("no counters were updated, skipping state update")
	}

	zap.L().Info("fetched counters")
	return nil
}

func (c *CounterFetcher) fetchInitialState() error {
	resident, err := c.apiClient.GetResident()
	if err != nil {
		return fmt.Errorf("failed to get resident: %w", err)
	}
	zap.L().Info("fetched resident",
		zap.String("first_name", resident.Resident.Nom),
		zap.String("id", resident.Resident.ID))

	if len(resident.Occupations) == 0 {
		return fmt.Errorf("no occupation found")
	}

	localID := resident.Occupations[0].LogementID
	zap.L().Info("found local", zap.String("local_id", localID))

	if len(resident.Occupations) > 1 {
		zap.L().Warn("multiple 'occupation' were found. please report this to the maintainer",
			zap.Int("occupation_count", len(resident.Occupations)))
	}

	local, err := c.apiClient.GetLocal(localID)
	if err != nil {
		return fmt.Errorf("failed to get local %s: %w", localID, err)
	}
	zap.L().Info("fetched local", zap.String("local_id", localID))

	if len(local.FluidesRestitues) == 0 {
		return fmt.Errorf("no fluid found for local %s", localID)
	}

	devices, err := c.fetchDevices(localID, len(local.FluidesRestitues))
	if err != nil {
		return fmt.Errorf("failed to reset counters: %w", err)
	}

	c.state.AccountData = rawAccountData{
		Resident: resident,
		Local:    local,
		Devices:  devices,
	}

	zap.L().Info("fetched initial state")
	return nil
}

// fetchDevices will grab the actual index of all counters.
//
// It does so by calling the API, but there's a catch: if a counter hasn't reported yet for the current day,
// it will be missing from the response. Thus, we need to go back 1 day earlier to get the last
func (c *CounterFetcher) fetchDevices(localID string, expectedDeviceCount int) ([]oceaapi.Device, error) {
	devices, err := c.apiClient.GetDevices(localID, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}

	if len(devices) == expectedDeviceCount {
		return devices, nil
	}

	yesterdayDevices, err := c.apiClient.GetDevices(localID, time.Now().AddDate(0, 0, -1))
	if err != nil {
		return nil, fmt.Errorf("failed to get yesterday devices: %w", err)
	}

	var completeList []oceaapi.Device

	// The current list doesn't contain all devices, so we're trying to back-fill the missing ones from the previous
	// statement (yesterdayDevices).
	for _, olderDevice := range yesterdayDevices {
		found := false

		// As we're working with day-1 measurements, we first check if there's an up to date measurement. If so
		// we keep the newest.
		for _, newerDevice := range devices {
			if olderDevice.AppareilID == newerDevice.AppareilID {
				completeList = append(completeList, newerDevice)
				found = true
				break
			}
		}

		if found {
			continue
		}

		// Otherwise, we default to the older measurement
		completeList = append(completeList, olderDevice)
	}

	if len(completeList) != expectedDeviceCount {
		return nil, fmt.Errorf("not enough devices")
	}

	return completeList, nil
}

func (c *CounterFetcher) updateCounters(devices []oceaapi.Device) (bool, error) {
	if len(c.state.CounterStates) == 0 {
		c.state.CounterStates = make([]CounterState, len(devices))
		for i, device := range devices {
			c.state.CounterStates[i] = CounterState{
				Fluid:         device.Fluide,
				AbsoluteIndex: device.ValeurIndex,
				SerialNumber:  device.NumeroCompteurAppareil,
			}
		}
		return true, nil
	}

	deviceFluidToDevice := map[string]oceaapi.Device{}
	for _, device := range devices {
		deviceFluidToDevice[device.Fluide] = device
	}

	updated := false
	for i, state := range c.state.CounterStates {
		device, ok := deviceFluidToDevice[state.Fluid]
		if !ok {
			return false, fmt.Errorf("no device for fluid %s", state.Fluid)
		}
		if state.AbsoluteIndex == device.ValeurIndex {
			continue
		}
		c.state.CounterStates[i].AbsoluteIndex = device.ValeurIndex
		updated = true
	}

	zap.L().Info("updated counters")
	return updated, nil
}
