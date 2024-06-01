package homeassistant

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
	"go.uber.org/zap"
)

type MQTTParams struct {
	Host     string
	Username string
	Password string
}

type MQTT struct {
	updates <-chan counterfetcher.Notification
	params  MQTTParams
	client  mqtt.Client
}

func New(params MQTTParams) (MQTT, chan<- counterfetcher.Notification) {
	listener := make(chan counterfetcher.Notification, 1)

	return MQTT{
		updates: listener,
		params:  params,
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

	var err error
	sensorConfigPublished := false

	for {
		if m.client == nil {
			m.client, err = m.buildClient()
			if err != nil {
				zap.L().Error("failed to build mqtt client", zap.Error(err))

				time.Sleep(60 * time.Second)
				continue
			}
		}

		update := <-m.updates

		if !sensorConfigPublished {
			m.publishSensorConfig(update)
			sensorConfigPublished = true
		}

		m.publishSensorValues(update)
	}
}

func (m *MQTT) publishSensorConfig(notif counterfetcher.Notification) {
	// Cleanup single-meter-per-fluid topics. To be removed in future versions.
	for fluid := range fluidDescriptions {
		topics := buildOldSensorTopics(fluid)
		m.client.Publish(topics.Config, 0, true, []byte{})
		m.client.Publish(topics.State, 0, true, []byte{})
	}
	zap.L().Info("cleared old topics")

	for _, state := range notif.CounterStates {
		topics, err := buildSensorTopics(state.Fluid, state.SerialNumber)
		if err != nil {
			zap.L().Error("failed to build sensor topics", zap.String("fluid", state.Fluid), zap.Error(err))
			continue
		}

		config, _ := getFluidSensorConfig(state.Fluid, state.SerialNumber, topics.State)

		payload, err := json.Marshal(config)
		if err != nil {
			zap.L().Error("failed to marshal json sensor config", zap.String("fluid", state.Fluid), zap.Error(err))
			continue
		}

		m.client.Publish(topics.Config, 1, true, payload)
		zap.L().Info("declared device", zap.String("fluid", state.Fluid))
	}
}

func (m *MQTT) publishSensorValues(notif counterfetcher.Notification) {
	for _, state := range notif.CounterStates {
		topics, err := buildSensorTopics(state.Fluid, state.SerialNumber)
		if err != nil {
			zap.L().Error("failed to build sensor topics", zap.String("fluid", state.Fluid), zap.Error(err))
			continue
		}

		payload := strconv.FormatFloat(state.AbsoluteIndex, 'f', -1, 64)

		m.client.Publish(topics.State, 1, true, payload)
		zap.L().Info("updated device", zap.String("fluid", state.Fluid), zap.String("value", payload))
	}
}

func (m *MQTT) buildClient() (mqtt.Client, error) {
	clientOptions := mqtt.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s", m.params.Host))

	if m.params.Password != "" {
		clientOptions = clientOptions.SetPassword(m.params.Password)
	}
	if m.params.Username != "" {
		clientOptions = clientOptions.SetUsername(m.params.Username)
	}

	client := mqtt.NewClient(clientOptions)

	token := client.Connect()
	token.WaitTimeout(10 * time.Second)

	if err := token.Error(); err != nil {
		client.Disconnect(0)
		return nil, fmt.Errorf("failed to connect to mqtt broker: %w", err)
	}

	return client, nil
}
