package counterfetcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
)

type state struct {
	CounterStates []CounterState `json:"counterStates"`
	AccountData   rawAccountData `json:"accountData"`
}

type stateStore interface {
	Load(string) (state, error)
	Save(string, state) error
}

type fileStateStore struct{}

func (fileStateStore) Load(file string) (state, error) { return loadState(file) }
func (fileStateStore) Save(file string, s state) error { return s.save(file) }

// save replaces the previous snapshot only after a complete, synced write.
func (s state) save(filePath string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".ocea-state-*")
	if err != nil {
		return fmt.Errorf("creating temporary state: %w", err)
	}
	defer os.Remove(temp.Name())
	// CreateTemp uses 0600; the new file never exposes account data publicly.
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("writing state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("syncing state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing state: %w", err)
	}
	if err := os.Rename(temp.Name(), filePath); err != nil {
		return fmt.Errorf("replacing state: %w", err)
	}
	return nil
}

type CounterState struct {
	Fluid         oceaapi.FluidName `json:"fluid"`
	AbsoluteIndex float64           `json:"absoluteIndex"`
	SerialNumber  string            `json:"serialNumber"`
	ReadingDate   string            `json:"readingDate,omitempty"`
}

func loadState(filePath string) (state, error) {
	data, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, fmt.Errorf("reading state: %w", err)
	}
	var snapshot state
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return state{}, fmt.Errorf("decoding state: %w", err)
	}
	return snapshot, nil
}

type rawAccountData struct {
	Resident oceaapi.Resident `json:"resident"`
	Local    oceaapi.Local    `json:"local"`
	Devices  []oceaapi.Device `json:"devices"`
}
