package counterfetcher

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
	"github.com/sywesk/ocea-exporter/pkg/oceaauth"
	"go.uber.org/zap"
)

var (
	errDashboardMissing   = fmt.Errorf("dashboard missing")
	errYearlyCounterReset = fmt.Errorf("yearly counter was reset")
	errMissingDevices     = fmt.Errorf("missing devices")
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
}

type Settings struct {
	StateFileLocation string
	Username          string
	Password          string
	PollInterval      time.Duration
}

func New(settings Settings) (*CounterFetcher, error) {
	if settings.StateFileLocation == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user config dir: %w", err)
		}

		settings.StateFileLocation = path.Join(dir, "ocea-exporter", "state.json")
	}

	return &CounterFetcher{
		settings: settings,
	}, nil
}

func (c *CounterFetcher) Start() error {
	var err error

	c.state, err = loadState(c.settings.StateFileLocation)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	tokenProvider := oceaauth.NewTokenProvider(c.settings.Username, c.settings.Password)
	c.apiClient = oceaapi.NewClient(tokenProvider)

	// If the state is empty, then we need to fetch everything first.
	if c.state.AccountData.Resident.NomClient == "" {
		err = c.fetchInitialState()
		if err != nil {
			return fmt.Errorf("failed to fetch initial state: %w", err)
		}
	}

	go c.worker()

	return nil
}

func (c *CounterFetcher) worker() {
	zap.L().Info("worker started")

	defer func() {
		if err := recover(); err != nil {
			zap.L().Error("worker crashed", zap.Any("panic_error", err))
			c.worker()
		}
	}()

	t := time.NewTicker(c.settings.PollInterval)

	for {
		err := c.fetchCounters()
		if err != nil {
			zap.L().Error("failed to fetch counters, will retry next time", zap.Error(err))
			c.healthy = false
		} else {
			c.healthy = true
			c.ready = true
		}

		<-t.C
	}
}

func (c *CounterFetcher) fetchCounters() error {
	localID := c.state.AccountData.Local.Local.ID

	var dashboards []oceaapi.Dashboard
	for _, fluid := range c.state.AccountData.Local.FluidesRestitues {
		dashboard, err := c.apiClient.GetFluidDashboard(localID, fluid.Fluide)
		if err != nil {
			return fmt.Errorf("failed to get dashboard for local %s and fluid %s: %w", localID, fluid.Fluide, err)
		}
		zap.L().Info("fetched fluid", zap.String("fluid", fluid.Fluide), zap.String("date", dashboard.DateDerniereReleve))

		dashboards = append(dashboards, dashboard)
	}

	c.state.AccountData.Dashboards = dashboards

	countersUpdated, err := c.updateCounters(dashboards)
	if errors.Is(err, errDashboardMissing) {
		// errDashboardMissing means that we've got a counter that doesn't have a counter anymore. This shouldn't happen on an account.
		// To solve the discrepancy, we must reset the state and and fetch everything again.
		err = c.fetchInitialState()
		if err != nil {
			return fmt.Errorf("updateCounters requested a full reset, but it failed: %w", err)
		}

		return nil
	} else if errors.Is(err, errYearlyCounterReset) {
		// We use "increasing" yearly counters to avoid fetching counter indexes too often (they are behind a 2nd authentication,
		// and I fear that it might get detected if called too often). But when they are reset on january first, we must start from
		// a clean state again to have a valid "yearly_counter / index" pair.
		err = c.resetCounters(dashboards)
		if errors.Is(err, errMissingDevices) {
			zap.L().Debug("some devices were missing, making the counter reset impossible. this is certainly because there were no measurements for the missing devices today, and we need to wait for them. this will be retried at the next check.")
			return nil
		} else if err != nil {
			return fmt.Errorf("updateCounters requested a counter reset, but it failed: %w", err)
		}

		return nil
	} else if err != nil {
		return fmt.Errorf("failed to update counters: %w", err)
	}

	if countersUpdated {
		err = c.state.save(c.settings.StateFileLocation)
		if err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
	} else {
		zap.L().Info("no counters were updated, skipping state update")
	}

	zap.L().Info("fetched counters")

	return nil
}

func (c *CounterFetcher) resetCounters(dashboards []oceaapi.Dashboard) error {
	localID := c.state.AccountData.Local.Local.ID

	devices, err := c.fetchDevices(localID, len(dashboards))
	if err != nil {
		return fmt.Errorf("failed to get devices for local %s: %w", localID, err)
	}
	zap.L().Info("fetched devices for local",
		zap.String("local_id", localID),
		zap.Int("device_count", len(devices)))

	err = c.initializeCounters(dashboards, devices)
	if err != nil {
		return fmt.Errorf("failed to initialize counters: %w", err)
	}

	err = c.state.save(c.settings.StateFileLocation)
	if err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	zap.L().Info("reset counters")

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
	zap.L().Info("fetched local",
		zap.String("local_id", localID))

	devices, err := c.fetchDevices(localID, len(local.FluidesRestitues))
	if err != nil {
		return fmt.Errorf("failed to get devices for local %s: %w", localID, err)
	}
	zap.L().Info("fetched devices for local",
		zap.String("local_id", localID),
		zap.Int("device_count", len(devices)))

	if len(local.FluidesRestitues) == 0 {
		return fmt.Errorf("no fluid found for local %s", localID)
	} else if len(local.FluidesRestitues) != len(devices) {
		return fmt.Errorf("the number of devices is different from the number of fluids (fuild_count=%d, device_count=%d)", len(local.FluidesRestitues), len(devices))
	}

	var dashboards []oceaapi.Dashboard
	for _, fluid := range local.FluidesRestitues {
		dashboard, err := c.apiClient.GetFluidDashboard(localID, fluid.Fluide)
		if err != nil {
			return fmt.Errorf("failed to get dashboard for local %s and fluid %s: %w", localID, fluid.Fluide, err)
		}
		zap.L().Info("fetched fluid", zap.String("fluid", fluid.Fluide))

		dashboards = append(dashboards, dashboard)
	}

	c.state.AccountData = rawAccountData{
		Resident:   resident,
		Local:      local,
		Dashboards: dashboards,
		Devices:    devices,
	}

	err = c.initializeCounters(dashboards, devices)
	if err != nil {
		return fmt.Errorf("failed to initialize counters: %w", err)
	}

	err = c.state.save(c.settings.StateFileLocation)
	if err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	zap.L().Info("fetched initial state")

	return nil
}

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

		completeList = append(completeList, olderDevice)
	}

	if len(completeList) != expectedDeviceCount {
		return nil, fmt.Errorf("not enough devices")
	}

	return completeList, nil
}

// updateCounters updates the counters by adding the difference between the last yearly index and the current
// one to the annual index. If the yearly counter is reset, we need to fetch the absolute indexes again.
func (c *CounterFetcher) updateCounters(dashboards []oceaapi.Dashboard) (bool, error) {
	localID := c.state.AccountData.Local.Local.ID

	dashByFluid := map[string]oceaapi.Dashboard{}
	for _, dashboard := range dashboards {
		dashByFluid[dashboard.Fluide] = dashboard
	}

	countersUpdated := false
	for i, state := range c.state.CounterStates {
		dashboard, ok := dashByFluid[state.Fluid]
		if !ok {
			zap.L().Warn("dashboard missing for counter", zap.String("fluid", state.Fluid))
			// If we can't find the dashboard, we need to start again from a clear state.
			return false, errDashboardMissing
		}

		currentAnnualIndex := round3(dashboard.ConsoCumuleeAnneeCourante)
		lastAnnualIndex := round3(state.AnnualIndex)

		if currentAnnualIndex < lastAnnualIndex {
			zap.L().Info("yearly counter was reset, triggering a full refresh")
			return false, errYearlyCounterReset
		}

		if currentAnnualIndex == lastAnnualIndex {
			zap.L().Debug("yearly counter hasn't changed", zap.String("fluid", state.Fluid))
		}

		c.state.CounterStates[i].AbsoluteIndex = round3(c.state.CounterStates[i].AbsoluteIndex + round3(currentAnnualIndex-lastAnnualIndex))
		c.state.CounterStates[i].AnnualIndex = currentAnnualIndex

		index.WithLabelValues(state.Fluid, localID).Set(c.state.CounterStates[i].AbsoluteIndex)
		countersUpdated = true
	}

	zap.L().Info("updated counters")

	return countersUpdated, nil
}

func (c *CounterFetcher) initializeCounters(dashboards []oceaapi.Dashboard, devices []oceaapi.Device) error {
	c.state.CounterStates = make([]counterState, len(devices))

	dashByFluid := map[string]oceaapi.Dashboard{}
	for _, dashboard := range dashboards {
		dashByFluid[dashboard.Fluide] = dashboard
	}

	for i, device := range devices {
		dashboard, ok := dashByFluid[device.Fluide]
		if !ok {
			return fmt.Errorf("devices %s refers to an unknown fluid %s", device.NumeroCompteurAppareil, device.Fluide)
		}

		c.state.CounterStates[i] = counterState{
			Fluid:         device.Fluide,
			AbsoluteIndex: device.ValeurIndex,
			AnnualIndex:   dashboard.ConsoCumuleeAnneeCourante,
		}
	}

	zap.L().Info("initialized counters")

	return nil
}
