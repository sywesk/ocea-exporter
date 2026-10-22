package homeassistant

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
)

// Exercise the real Paho client against a local MQTT peer, including QoS 1
// acknowledgements. No external broker or credentials are required.
func TestRealMQTTDiscoveryAndState(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	publications := make(chan *packets.PublishPacket, 16)
	serverDone := make(chan error, 1)
	go func() { serverDone <- serveMQTTPeer(listener, accepted, publications) }()
	t.Cleanup(func() {
		listener.Close()
		select {
		case conn := <-accepted:
			conn.Close()
		default:
		}
		select {
		case <-serverDone:
		case <-time.After(2 * time.Second):
			t.Error("local MQTT peer did not stop")
		}
	})
	m, _ := New(MQTTParams{Host: listener.Addr().String()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.ctx = ctx
	defer m.disconnect()
	update := counterfetcher.Notification{CounterStates: []counterfetcher.CounterState{{SerialNumber: "a", Fluid: "EauFroide", AbsoluteIndex: 1.235}}}
	if err := m.publishSnapshot(ctx, update); err != nil {
		t.Fatal(err)
	}
	var config, state bool
	for len(publications) > 0 {
		p := <-publications
		if !p.Retain || p.Qos != 1 {
			t.Fatal("publication did not request retained QoS 1 delivery")
		}
		if p.TopicName == "homeassistant/sensor/ocea_exporter/water_meter_a/config" {
			config = true
		}
		if p.TopicName == "homeassistant/sensor/ocea_exporter/water_meter_a/state" && string(p.Payload) == "1.235" {
			state = true
		}
	}
	if !config || !state {
		t.Fatal("discovery or meter value missing")
	}
}

func serveMQTTPeer(listener net.Listener, accepted chan<- net.Conn, publications chan<- *packets.PublishPacket) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	accepted <- conn
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	for {
		packet, err := packets.ReadPacket(conn)
		if err != nil {
			return err
		}
		switch p := packet.(type) {
		case *packets.ConnectPacket:
			err = packets.NewControlPacket(packets.Connack).Write(conn)
		case *packets.PublishPacket:
			publications <- p
			ack := packets.NewControlPacket(packets.Puback).(*packets.PubackPacket)
			ack.MessageID = p.MessageID
			err = ack.Write(conn)
		case *packets.DisconnectPacket:
			return nil
		default:
			return fmt.Errorf("unexpected MQTT packet %T", packet)
		}
		if err != nil {
			return err
		}
	}
}
