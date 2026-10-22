package homeassistant

import (
	"context"
	"errors"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
)

type fakeToken struct {
	err       error
	completed bool
	waits     int
}

func (t *fakeToken) Wait() bool                     { t.waits++; return t.completed }
func (t *fakeToken) WaitTimeout(time.Duration) bool { t.waits++; return t.completed }
func (t *fakeToken) Done() <-chan struct{} {
	t.waits++
	ch := make(chan struct{})
	if t.completed {
		close(ch)
	}
	return ch
}
func (t *fakeToken) Error() error { return t.err }

type publication struct {
	topic    string
	retained bool
	payload  interface{}
}
type fakeClient struct {
	mqtt.Client
	token        *fakeToken
	publications []publication
	disconnected bool
	onPublish    func(string, interface{})
}

func (c *fakeClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	c.publications = append(c.publications, publication{topic, retained, payload})
	if c.onPublish != nil {
		c.onPublish(topic, payload)
	}
	return c.token
}
func (c *fakeClient) Connect() mqtt.Token    { return c.token }
func (c *fakeClient) Disconnect(uint)        { c.disconnected = true }
func (c *fakeClient) IsConnectionOpen() bool { return !c.disconnected }

func TestPublicationsWaitForAcknowledgement(t *testing.T) {
	client := &fakeClient{token: &fakeToken{completed: true}}
	m := MQTT{client: client}
	notif := counterfetcher.Notification{CounterStates: []counterfetcher.CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 1.235}}}
	m.publishSensorConfig(notif)
	m.publishSensorValues(notif)
	if client.token.waits != len(client.publications) {
		t.Fatalf("%d acknowledgements checked for %d publications", client.token.waits, len(client.publications))
	}
	for _, p := range client.publications {
		if !p.retained {
			t.Fatal("publication is not retained")
		}
	}
	last := client.publications[len(client.publications)-1]
	if last.payload != "1.235" {
		t.Fatalf("wrong published value: %v", last.payload)
	}
}

func TestUnknownFluidIsNotPublished(t *testing.T) {
	client := &fakeClient{token: &fakeToken{completed: true, err: errors.New("offline")}}
	m := MQTT{client: client}
	m.publishSensorValues(counterfetcher.Notification{CounterStates: []counterfetcher.CounterState{{SerialNumber: "a", Fluid: "unknown"}}})
	if len(client.publications) != 0 {
		t.Fatal("unknown fluid published")
	}
}

func TestDiscoveryRetriesFailuresAndRemovesRetiredMeters(t *testing.T) {
	client := &fakeClient{token: &fakeToken{completed: true, err: errors.New("offline")}}
	m, _ := New(MQTTParams{Host: "localhost"})
	m.client = client
	notif := counterfetcher.Notification{CounterStates: []counterfetcher.CounterState{{SerialNumber: "old", Fluid: "EauFroide"}}}
	if err := m.publishSensorConfig(notif); err == nil {
		t.Fatal("publication failure hidden")
	}
	client.token = &fakeToken{completed: true}
	if err := m.publishSensorConfig(notif); err != nil {
		t.Fatal(err)
	}
	before := len(client.publications)
	notif.CounterStates[0].SerialNumber = "new"
	if err := m.publishSensorConfig(notif); err != nil {
		t.Fatal(err)
	}
	var added, removed bool
	for _, p := range client.publications[before:] {
		if p.topic == "homeassistant/sensor/ocea_exporter/water_meter_new/config" {
			added = true
		}
		if p.topic == "homeassistant/sensor/ocea_exporter/water_meter_old/config" {
			payload, ok := p.payload.([]byte)
			removed = ok && len(payload) == 0
		}
	}
	if !added || !removed {
		t.Fatalf("discovery not reconciled: added=%v removed=%v", added, removed)
	}
}

func TestConnectionTimeoutDoesNotReturnClient(t *testing.T) {
	for _, token := range []*fakeToken{{}, {completed: true, err: errors.New("refused")}} {
		m, _ := New(MQTTParams{Host: "localhost"})
		client := &fakeClient{token: token}
		m.newClient = func(*mqtt.ClientOptions) mqttClient { return client }
		m.timeout = time.Millisecond
		if got, err := m.buildClient(context.Background()); err == nil || got != nil {
			t.Fatal("failed connection returned a client")
		}
		if !client.disconnected {
			t.Fatal("failed connection not closed")
		}
	}
}

func TestPendingMQTTOperationIsCancelled(t *testing.T) {
	m, _ := New(MQTTParams{Host: "localhost"})
	m.newClient = func(*mqtt.ClientOptions) mqttClient { return &fakeClient{token: &fakeToken{}} }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.buildClient(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestBrokerAddresses(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"localhost", "tcp://localhost:1883"}, {"localhost:1884", "tcp://localhost:1884"}, {"::1", "tcp://[::1]:1883"}, {"[::1]", "tcp://[::1]:1883"}, {"ssl://broker:8883", "ssl://broker:8883"}} {
		got, err := brokerURL(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", "http://broker", "host:not-a-port"} {
		if _, err := brokerURL(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestWorkerReconnectsAndPublishesLatestSnapshot(t *testing.T) {
	m, updates := New(MQTTParams{Host: "localhost"})
	m.retryInterval = 10 * time.Millisecond
	failed := make(chan struct{}, 1)
	published := make(chan struct{}, 1)
	first := &fakeClient{token: &fakeToken{completed: true, err: errors.New("refused")}}
	second := &fakeClient{token: &fakeToken{completed: true}, onPublish: func(topic string, payload interface{}) {
		if topic == "homeassistant/sensor/ocea_exporter/water_meter_a/state" && payload == "2" {
			select {
			case published <- struct{}{}:
			default:
			}
		}
	}}
	attempts := 0
	m.newClient = func(*mqtt.ClientOptions) mqttClient {
		attempts++
		if attempts == 1 {
			failed <- struct{}{}
			return first
		}
		return second
	}
	ctx, cancel := context.WithCancel(context.Background())
	go m.Run(ctx)
	t.Cleanup(func() { cancel(); <-m.Done() })
	updates <- counterfetcher.Notification{CounterStates: []counterfetcher.CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 1}}}
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("connection was not attempted")
	}
	updates <- counterfetcher.Notification{CounterStates: []counterfetcher.CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 2}}}
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("latest snapshot was not retried")
	}
}

func TestWorkerStopsWhileWaitingForUpdates(t *testing.T) {
	m, _ := New(MQTTParams{Host: "localhost"})
	ctx, cancel := context.WithCancel(context.Background())
	go m.Run(ctx)
	cancel()
	select {
	case <-m.Done():
	case <-time.After(time.Second):
		t.Fatal("idle MQTT worker did not stop")
	}
}

func TestUnsupportedFluidDoesNotBlockKnownMeters(t *testing.T) {
	client := &fakeClient{token: &fakeToken{completed: true}}
	m, _ := New(MQTTParams{Host: "localhost"})
	m.client = client
	update := counterfetcher.Notification{CounterStates: []counterfetcher.CounterState{{SerialNumber: "x", Fluid: "unsupported"}, {SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 2}}}
	if err := m.publishSensorConfig(update); err != nil {
		t.Fatal(err)
	}
	if err := m.publishSensorValues(update); err != nil {
		t.Fatal(err)
	}
	last := client.publications[len(client.publications)-1]
	if last.payload != "2" {
		t.Fatal("known meter was not published")
	}
}
