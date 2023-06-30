package homeassistant

import (
	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
	"go.uber.org/zap"
)

type MQTTParams struct {
	Host     string
	Username string
	Password string
}

type MQTT struct {
	updates <-chan []counterfetcher.CounterState
}

func New(params MQTTParams) (MQTT, chan<- []counterfetcher.CounterState) {
	listener := make(chan []counterfetcher.CounterState, 1)

	return MQTT{
		updates: listener,
	}, listener
}

func (m *MQTT) Start() {
	go m.worker()
}

func (m *MQTT) worker() {
	zap.L().Info("mqtt worker started")

	defer func() {
		if err := recover(); err != nil {
			zap.L().Error("mqtt worker crashed", zap.Any("panic_error", err))
			m.worker()
		}
	}()

	for {
		update := <-m.updates

	}
}
