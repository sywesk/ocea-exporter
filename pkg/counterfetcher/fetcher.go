package counterfetcher

import (
	"fmt"
	"os"
	"path"
)

type CounterFetcher struct {
	settings Settings
	state    state
}

type Settings struct {
	StateFileLocation string
	Username          string
	Password          string
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
