package counterfetcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
	"go.uber.org/zap"
)

type state struct {
	CounterStates []CounterState `json:"counterStates"`
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

type CounterState struct {
	Fluid         string  `json:"fluid"`
	AbsoluteIndex float64 `json:"absoluteIndex"`
	SerialNumber  string  `json:"serialNumber"`
}

func (c CounterState) Clone() CounterState {
	return CounterState{
		Fluid:         c.Fluid,
		AbsoluteIndex: c.AbsoluteIndex,
		SerialNumber:  c.SerialNumber,
	}
}

func loadState(path string) (state, error) {
	if _, err := os.Stat(path); err != nil {
		zap.L().Info("state file not found, skipping load", zap.String("path", path))
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
	Resident oceaapi.Resident `json:"resident"`
	Local    oceaapi.Local    `json:"local"`
	Devices  []oceaapi.Device `json:"devices"`
}
