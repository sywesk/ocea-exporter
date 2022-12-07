package counterfetcher

import (
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"os"
)

type state struct {
	CounterStates []counterState `json:"counterStates"`
}

func (s state) save(path string) error {
	// TODO
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
	AbsoluteIndex        float64 `json:"absoluteIndex"`
	YtDRelativeIndex     float64 `json:"ytDRelativeIndex"`
	LastYtDRelativeIndex float64 `json:"lastYtDRelativeIndex"`
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

	return diskState, nil
}
