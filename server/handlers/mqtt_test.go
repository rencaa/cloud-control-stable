package handlers

import (
	"bytes"
	"encoding/json"
	"net"
	"testing"
)

func TestMQTTRemainingLengthRoundTrip(t *testing.T) {
	for _, value := range []int{0, 1, 127, 128, 321, 16384, 65536} {
		packet := writeMQTTPacket(0x30, bytes.Repeat([]byte{'x'}, value))
		decoded, err := readMQTTPacket(bytes.NewReader(packet))
		if err != nil {
			t.Fatalf("value %d: %v", value, err)
		}
		if len(decoded.body) != value {
			t.Fatalf("value %d: got %d", value, len(decoded.body))
		}
	}

	encoded := make([]byte, 4)
	if got := encodeMQTTRemainingLength(encoded, 268435455); got != 4 {
		t.Fatalf("max remaining length encoded with %d bytes", got)
	}
}

func TestMQTTTopicMatches(t *testing.T) {
	tests := []struct {
		filter string
		topic  string
		want   bool
	}{
		{"cloud/device/+/command", "cloud/device/dev-1/command", true},
		{"cloud/device/+/command", "cloud/device/dev-1/event", false},
		{"device/#", "device/dev-1/event", true},
		{"device/#", "cloud/device/dev-1/event", false},
		{"#", "anything/here", true},
	}
	for _, test := range tests {
		if got := mqttTopicMatches(test.filter, test.topic); got != test.want {
			t.Fatalf("%s vs %s: got %v want %v", test.filter, test.topic, got, test.want)
		}
	}
	if !deviceTopicAllowed("dev-1", "device/dev-1/event") {
		t.Fatal("legacy event topic rejected")
	}
}

func TestMQTTBrokerConnectSubscribePing(t *testing.T) {
	broker := NewMQTTBroker(nil)
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		broker.serveClient(serverConn)
		close(done)
	}()
	defer func() {
		_ = clientConn.Close()
		<-done
	}()

	_, _ = clientConn.Write(writeMQTTPacket(0x10, testMQTTConnectBody("test-device")))
	packet, err := readMQTTPacket(clientConn)
	if err != nil || packet.header != 0x20 || len(packet.body) != 2 || packet.body[1] != 0 {
		t.Fatalf("CONNACK failed: header=0x%02x err=%v", packet.header, err)
	}

	_, _ = clientConn.Write(writeMQTTPacket(
		0x82,
		testMQTTSubscribeBody(1, "cloud/device/test-device/command"),
	))
	packet, err = readMQTTPacket(clientConn)
	if err != nil || packet.header != 0x90 {
		t.Fatalf("SUBACK failed: header=0x%02x err=%v", packet.header, err)
	}

	_, _ = clientConn.Write(writeMQTTPacket(0xC0, nil))
	packet, err = readMQTTPacket(clientConn)
	if err != nil || packet.header != 0xD0 {
		t.Fatalf("PINGRESP failed: header=0x%02x err=%v", packet.header, err)
	}

	_, _ = clientConn.Write(writeMQTTPacket(0xE0, nil))
}

func TestMQTTBrokerBridgesHubCommand(t *testing.T) {
	hub := NewWSHub(nil)
	broker := NewMQTTBroker(hub)
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		broker.serveClient(serverConn)
		close(done)
	}()
	defer func() {
		_ = clientConn.Close()
		<-done
		<-hub.unregister
	}()

	_, _ = clientConn.Write(writeMQTTPacket(0x10, testMQTTConnectBody("bridge-device")))
	packet, err := readMQTTPacket(clientConn)
	if err != nil || packet.header != 0x20 {
		t.Fatalf("CONNACK failed: header=0x%02x err=%v", packet.header, err)
	}

	hubClient := <-hub.register
	hub.mu.Lock()
	hub.clients[hubClient.DeviceID] = hubClient
	hub.mu.Unlock()

	_, _ = clientConn.Write(writeMQTTPacket(
		0x82,
		testMQTTSubscribeBody(1, "cloud/device/bridge-device/command"),
	))
	_, err = readMQTTPacket(clientConn)
	if err != nil {
		t.Fatal(err)
	}

	message := WSMessage{
		Type: "command",
		Data: map[string]interface{}{
			"cmd_id":  "bridge-cmd-1",
			"command": "ping",
		},
	}
	if err := hub.SendToDevice("bridge-device", message); err != nil {
		t.Fatal(err)
	}
	packet, err = readMQTTPacket(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	publish, err := parseMQTTPublish(packet.header, packet.body)
	if err != nil {
		t.Fatal(err)
	}
	if publish.topic != "cloud/device/bridge-device/command" {
		t.Fatalf("topic=%q", publish.topic)
	}
	var decoded WSMessage
	if err := json.Unmarshal(publish.payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "command" {
		t.Fatalf("message type=%q", decoded.Type)
	}

	_, _ = clientConn.Write(writeMQTTPacket(0xE0, nil))
}

func testMQTTConnectBody(clientID string) []byte {
	body := appendMQTTUTF8(nil, "MQTT")
	body = append(body, 4, 2, 0, 60)
	return appendMQTTUTF8(body, clientID)
}

func testMQTTSubscribeBody(packetID uint16, filter string) []byte {
	body := []byte{byte(packetID >> 8), byte(packetID)}
	body = appendMQTTUTF8(body, filter)
	return append(body, 0)
}
