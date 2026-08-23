package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud-control-server/config"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// ExternalMQTTBridge connects the API process to an external broker while the
// existing WSHub remains the single business-message handler.
type ExternalMQTTBridge struct {
	hub    *WSHub
	client paho.Client
}

func NewExternalMQTTBridge(hub *WSHub) (*ExternalMQTTBridge, error) {
	if hub == nil || config.App == nil || strings.TrimSpace(config.App.MQTT.BrokerURL) == "" {
		return nil, fmt.Errorf("external mqtt is not configured")
	}
	bridge := &ExternalMQTTBridge{hub: hub}
	clientID := strings.TrimSpace(config.App.MQTT.ClientID)
	if clientID == "" {
		clientID = fmt.Sprintf("cloud-control-%d", time.Now().UnixNano())
	}
	opts := paho.NewClientOptions().
		AddBroker(config.App.MQTT.BrokerURL).
		SetClientID(clientID).
		SetCleanSession(false).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetOrderMatters(false)
	if config.App.MQTT.Username != "" {
		opts.SetUsername(config.App.MQTT.Username)
		opts.SetPassword(config.App.MQTT.Password)
	}
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		log.Printf("External MQTT connection lost: %v", err)
	})
	opts.SetOnConnectHandler(func(client paho.Client) {
		filters := map[string]byte{
			"cloud/device/+/event":  1,
			"cloud/device/+/status": 1,
			"cloud/device/+/ack":    1,
		}
		token := client.SubscribeMultiple(filters, bridge.handleMessage)
		if !token.WaitTimeout(10*time.Second) || token.Error() != nil {
			log.Printf("External MQTT subscribe failed: %v", token.Error())
			return
		}
		log.Printf("External MQTT subscriptions ready")
	})
	bridge.client = paho.NewClient(opts)
	return bridge, nil
}

func (b *ExternalMQTTBridge) Start() error {
	if b == nil || b.client == nil {
		return fmt.Errorf("external mqtt bridge unavailable")
	}
	token := b.client.Connect()
	if !token.WaitTimeout(15 * time.Second) {
		return fmt.Errorf("external mqtt connect timeout")
	}
	return token.Error()
}

func (b *ExternalMQTTBridge) Stop() {
	if b != nil && b.client != nil && b.client.IsConnected() {
		b.client.Disconnect(1000)
	}
}

func (b *ExternalMQTTBridge) PublishCommand(deviceID string, payload []byte) error {
	if b == nil || b.client == nil || !b.client.IsConnectionOpen() {
		return ErrDeviceOffline
	}
	topic := "cloud/device/" + deviceID + "/command"
	token := b.client.Publish(topic, 1, false, payload)
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("external mqtt publish timeout")
	}
	if err := token.Error(); err != nil {
		return err
	}
	return nil
}

func (b *ExternalMQTTBridge) handleMessage(_ paho.Client, message paho.Message) {
	parts := strings.Split(message.Topic(), "/")
	if len(parts) != 4 || parts[0] != "cloud" || parts[1] != "device" || parts[2] == "" {
		return
	}
	deviceID := parts[2]
	if !validDeviceID(deviceID) {
		return
	}
	b.hub.TouchExternalDevice(deviceID)
	var envelope WSMessage
	if err := json.Unmarshal(message.Payload(), &envelope); err != nil {
		log.Printf("External MQTT invalid message device=%s: %v", deviceID, err)
		return
	}
	client := &WSClient{
		DeviceID: deviceID,
		Send:     make(chan []byte, 1),
		Virtual:  true,
	}
	client.handleMessage(envelope)
}
