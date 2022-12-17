package counterfetcher

import (
	"encoding/json"
	"fmt"
	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
	"go.uber.org/zap"
	"os"
	"path"
)

type state struct {
	CounterStates []counterState `json:"counterStates"`
	AccountData   rawAccountData `json:"accountData"`
}

func (s state) save(filePath string) error {
	bytes, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	err = os.MkdirAll(path.Dir(filePath), 0600)
	if err != nil {
		return fmt.Errorf("failed to mkdirall: %w", err)
	}

	err = os.WriteFile(filePath, bytes, 0600)
	if err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	zap.L().Info("state successfully written", zap.String("path", filePath))

	return nil
}

// counterState helps avoid getting the absolute indexes too often.
// To do so, it remembers the pair (AbsoluteIndex, YtDRelativeIndex), which represents the cumulative value at a given
// time and its corresponding year-to-date value, the latter of which isn't protected by a specific endpoint.
// Then, to get the current absolute index, we subtract the state's year-to-date index to the current one, and add the
// difference to the absolute index.
//
// TL;DR: with t2 > t1, absoluteIndex(t2) = absoluteIndex(t1) + (ytdIndex(t2) - ytdIndex(t1))
//
// We also need to periodically save the current relative index (aka LastYtDRelativeIndex) in order to detect when the
// value is reset at the beginning of the year.
type counterState struct {
	Fluid         string  `json:"fluid"`
	AbsoluteIndex float64 `json:"absoluteIndex"`
	AnnualIndex   float64 `json:"annualIndex"`
}

func loadState(path string) (state, error) {
	if _, err := os.Stat(path); err != nil {
		zap.L().Info("log file not found, skipping load", zap.String("path", path))
		return state{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return state{}, fmt.Errorf("failed to read state file: %w", err)
	}

	var diskState state
	err = json.Unmarshal(data, &diskState)
	if err != nil {
		return state{}, fmt.Errorf("failed to unmarshal state file: %w", err)
	}

	zap.L().Info("state successfully loaded", zap.String("path", path))

	return diskState, nil
}

type rawAccountData struct {
	Resident   oceaapi.Resident    `json:"resident"`
	Local      oceaapi.Local       `json:"local"`
	Dashboards []oceaapi.Dashboard `json:"dashboards"`
	Devices    []oceaapi.Device    `json:"devices"`
}
