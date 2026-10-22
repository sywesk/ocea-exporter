package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
	"go.uber.org/zap"
)

type MQTTParams struct{ Host, Username, Password string }

func (p MQTTParams) Validate() error { _, err := brokerURL(p.Host); return err }

type mqttClient interface {
	Connect() mqtt.Token
	Publish(string, byte, bool, interface{}) mqtt.Token
	Disconnect(uint)
	IsConnectionOpen() bool
}

type MQTT struct {
	updates       <-chan counterfetcher.Notification
	params        MQTTParams
	client        mqttClient
	newClient     func(*mqtt.ClientOptions) mqttClient
	timeout       time.Duration
	retryInterval time.Duration
	ctx           context.Context
	done          chan struct{}
	discovered    map[string]SensorTopics
	legacyCleared bool
}

func New(params MQTTParams) (MQTT, chan<- counterfetcher.Notification) {
	updates := make(chan counterfetcher.Notification, 1)
	return MQTT{updates: updates, params: params, timeout: 10 * time.Second, retryInterval: time.Minute, done: make(chan struct{})}, updates
}

// Run owns the MQTT connection and retries the latest snapshot until acknowledged.
func (m *MQTT) Run(ctx context.Context) {
	m.ctx = ctx
	defer close(m.done)
	defer m.disconnect()
	var pending *counterfetcher.Notification
	for {
		if ctx.Err() != nil {
			return
		}
		if pending == nil {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-m.updates:
				if !ok {
					return
				}
				pending = &update
			}
		}
		// Drop buffered obsolete snapshots after a broker outage.
		select {
		case update, ok := <-m.updates:
			if !ok {
				return
			}
			pending = &update
		default:
		}
		if err := m.publishSnapshot(ctx, *pending); err != nil {
			zap.L().Error("MQTT update failed; will retry latest snapshot", zap.Error(err))
			m.disconnect()
			var keepRunning bool
			pending, keepRunning = m.waitRetry(ctx, pending)
			if !keepRunning {
				return
			}
			continue
		}
		pending = nil
	}
}

// Keep accepting updates during the retry delay, so a full channel cannot
// preserve an obsolete reading for the entire duration of an outage.
func (m *MQTT) waitRetry(ctx context.Context, pending *counterfetcher.Notification) (*counterfetcher.Notification, bool) {
	timer := time.NewTimer(m.retryInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, false
		case <-timer.C:
			return pending, true
		case update, ok := <-m.updates:
			if !ok {
				return nil, false
			}
			pending = &update
		}
	}
}

func (m *MQTT) Done() <-chan struct{} { return m.done }

func (m *MQTT) disconnect() {
	if m.client != nil {
		m.client.Disconnect(0)
		m.client = nil
	}
}

func (m *MQTT) publishSnapshot(ctx context.Context, notification counterfetcher.Notification) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("MQTT update panicked: %v", p)
		}
	}()

	if m.client == nil || !m.client.IsConnectionOpen() {
		m.disconnect()
		client, err := m.buildClient(ctx)
		if err != nil {
			return err
		}
		m.client = client
	}
	if err := m.publishSensorConfig(notification); err != nil {
		return err
	}
	return m.publishSensorValues(notification)
}

func (m *MQTT) publishSensorConfig(notification counterfetcher.Notification) error {
	if !m.legacyCleared {
		for fluid := range fluidDescriptions {
			topics := buildOldSensorTopics(fluid)
			if err := m.publish(topics.Config, []byte{}); err != nil {
				return err
			}
			if err := m.publish(topics.State, []byte{}); err != nil {
				return err
			}
		}
		m.legacyCleared = true
	}
	if m.discovered == nil {
		m.discovered = make(map[string]SensorTopics)
	}
	current := make(map[string]bool)
	for _, state := range notification.CounterStates {
		topics, err := buildSensorTopics(state.Fluid, state.SerialNumber)
		if errors.Is(err, ErrUnknownFluid) {
			zap.L().Warn("unsupported Home Assistant fluid", zap.String("fluid", string(state.Fluid)))
			continue
		}
		if err != nil {
			return err
		}
		config, err := getFluidSensorConfig(state.Fluid, state.SerialNumber, topics.State)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("encoding discovery: %w", err)
		}
		// Retained configs are republished after reconnects and inventory changes.
		if err := m.publish(topics.Config, payload); err != nil {
			return err
		}
		m.discovered[topics.Config] = topics
		current[topics.Config] = true
	}
	for key, topics := range m.discovered {
		if current[key] {
			continue
		}
		if err := m.publish(topics.Config, []byte{}); err != nil {
			return err
		}
		if err := m.publish(topics.State, []byte{}); err != nil {
			return err
		}
		delete(m.discovered, key)
	}
	return nil
}

func (m *MQTT) publishSensorValues(notification counterfetcher.Notification) error {
	for _, state := range notification.CounterStates {
		topics, err := buildSensorTopics(state.Fluid, state.SerialNumber)
		if errors.Is(err, ErrUnknownFluid) {
			zap.L().Warn("unsupported Home Assistant fluid", zap.String("fluid", string(state.Fluid)))
			continue
		}
		if err != nil {
			return err
		}
		if math.IsNaN(state.AbsoluteIndex) || math.IsInf(state.AbsoluteIndex, 0) {
			return fmt.Errorf("invalid value for meter %s", state.SerialNumber)
		}
		if err := m.publish(topics.State, strconv.FormatFloat(state.AbsoluteIndex, 'f', -1, 64)); err != nil {
			return err
		}
	}
	return nil
}

func (m *MQTT) publish(topic string, payload interface{}) error {
	if err := m.waitToken(m.context(), m.client.Publish(topic, 1, true, payload)); err != nil {
		return fmt.Errorf("publishing %s: %w", topic, err)
	}
	return nil
}

func (m *MQTT) context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *MQTT) waitToken(ctx context.Context, token mqtt.Token) error {
	timeout := m.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("MQTT operation timed out after %s", timeout)
	case <-token.Done():
		return token.Error()
	}
}

func (m *MQTT) buildClient(ctx context.Context) (mqttClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, err := brokerURL(m.params.Host)
	if err != nil {
		return nil, err
	}
	options := mqtt.NewClientOptions().AddBroker(endpoint).SetUsername(m.params.Username).SetPassword(m.params.Password).
		SetConnectTimeout(10 * time.Second).SetWriteTimeout(10 * time.Second).SetAutoReconnect(false).SetConnectRetry(false)
	factory := m.newClient
	if factory == nil {
		factory = func(options *mqtt.ClientOptions) mqttClient { return mqtt.NewClient(options) }
	}
	client := factory(options)
	if err := m.waitToken(ctx, client.Connect()); err != nil {
		client.Disconnect(0)
		return nil, fmt.Errorf("connecting to MQTT: %w", err)
	}
	return client, nil
}

func brokerURL(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("empty MQTT broker address")
	}
	if !strings.Contains(address, "://") {
		host := strings.Trim(address, "[]")
		if net.ParseIP(host) != nil {
			address = net.JoinHostPort(host, "1883")
		} else if !strings.Contains(address, ":") {
			address = net.JoinHostPort(address, "1883")
		}
		address = "tcp://" + address
	}
	endpoint, err := url.Parse(address)
	if err != nil {
		return "", fmt.Errorf("invalid MQTT address: %w", err)
	}
	switch endpoint.Scheme {
	case "tcp", "ssl", "tls", "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported MQTT scheme %q", endpoint.Scheme)
	}
	if (endpoint.Scheme == "tcp" || endpoint.Scheme == "ssl" || endpoint.Scheme == "tls") && (endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "") {
		return "", fmt.Errorf("TCP MQTT address must not contain a path, query, or fragment")
	}
	if endpoint.Hostname() == "" || endpoint.User != nil {
		return "", fmt.Errorf("invalid MQTT host")
	}
	if endpoint.Port() == "" {
		port := "1883"
		switch endpoint.Scheme {
		case "ssl", "tls":
			port = "8883"
		case "ws":
			port = "80"
		case "wss":
			port = "443"
		}
		endpoint.Host = net.JoinHostPort(endpoint.Hostname(), port)
	}
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", fmt.Errorf("invalid MQTT port")
	}
	return endpoint.String(), nil
}
