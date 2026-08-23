package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"
)

const (
	mqttMaxPacketSize   = 2 * 1024 * 1024
	mqttKeepAliveGrace  = 15 * time.Second
	mqttSendQueueSize   = 512
	mqttMaxInflightQoS1 = 128
	mqttQoS1Retry       = 15 * time.Second
	mqttQoS1MaxAttempts = 3
)

var (
	errMQTTMalformed   = errors.New("malformed mqtt packet")
	errMQTTUnsupported = errors.New("unsupported mqtt feature")
)

type mqttPacket struct {
	header byte
	body   []byte
}

type mqttSubscription struct {
	filter string
	qos    byte
}

type mqttPublish struct {
	topic    string
	payload  []byte
	qos      byte
	packetID uint16
}

// MQTTBroker 是嵌入云控主服务的最小 MQTT 3.1.1 Broker。
// 设备进入 MQTT 后仍复用 WSHub，因此任务、命令、心跳、截图和短信
// 不需要在业务层复制一套 MQTT 处理逻辑。
type MQTTBroker struct {
	hub *WSHub

	mu       sync.RWMutex
	clients  map[string]*mqttDevice
	stop     chan struct{}
	stopOnce sync.Once
	listener net.Listener
}

type mqttDevice struct {
	id     string
	conn   net.Conn
	broker *MQTTBroker
	send   chan []byte
	done   chan struct{}

	hubClient *WSClient

	closeOnce sync.Once
	subMu     sync.RWMutex
	subs      map[string]byte

	packetMu     sync.Mutex
	nextPacketID uint16
	pendingQoS1  map[uint16]mqttPendingPublish
}

type mqttPendingPublish struct {
	packet   []byte
	sentAt   time.Time
	attempts int
}

func NewMQTTBroker(hub *WSHub) *MQTTBroker {
	return &MQTTBroker{
		hub:     hub,
		clients: make(map[string]*mqttDevice),
		stop:    make(chan struct{}),
	}
}

// Serve 接受 MQTT TCP 连接。每台设备一个读协程和一个写协程。
func (b *MQTTBroker) Serve(listener net.Listener) error {
	b.mu.Lock()
	b.listener = listener
	b.mu.Unlock()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-b.stop:
				return nil
			default:
			}
			return err
		}
		go b.serveClient(conn)
	}
}

func (b *MQTTBroker) serveClient(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
		_ = tcp.SetNoDelay(true)
	}
	connect, err := readMQTTConnectInfo(conn)
	if err != nil {
		_ = conn.Close()
		return
	}
	if (b.hub == nil || b.hub.db == nil) && config.App != nil && (config.App.MQTT.Username != "" || config.App.MQTT.Password != "") {
		userOK := subtle.ConstantTimeCompare([]byte(connect.username), []byte(config.App.MQTT.Username)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(connect.password), []byte(config.App.MQTT.Password)) == 1
		if !connect.usernameSet || !connect.passwordSet || !userOK || !passOK {
			_, _ = conn.Write(makeMQTTConnack(4))
			_ = conn.Close()
			return
		}
	}
	clientID, keepAlive := connect.clientID, connect.keepAlive
	deviceUserID := uint64(0)
	if b.hub != nil && b.hub.db != nil && (config.App == nil || config.App.Security.DeviceAuthRequired) {
		// Production MQTT clients use the device id as username and the
		// per-device token as password. This also prevents subscribing to
		// another device's command/event topics.
		if !connect.usernameSet || connect.username != clientID || !connect.passwordSet {
			_, _ = conn.Write(makeMQTTConnack(4))
			_ = conn.Close()
			return
		}
		deviceModel, ok := AuthenticateDevice(b.hub.db, clientID, connect.password)
		if !ok {
			_, _ = conn.Write(makeMQTTConnack(4))
			_ = conn.Close()
			return
		}
		deviceUserID = deviceModel.UserID
	} else if b.hub != nil && b.hub.db != nil {
		// Address-trust mode intentionally accepts legacy MQTT clients that do
		// not send username/password. Preserve the owner when the device is
		// already known; new devices are attached by their register message.
		var deviceModel models.Device
		if err := b.hub.db.Where("device_id = ?", clientID).First(&deviceModel).Error; err == nil {
			deviceUserID = deviceModel.UserID
		}
	}

	device := &mqttDevice{
		id:          clientID,
		conn:        conn,
		broker:      b,
		send:        make(chan []byte, mqttSendQueueSize),
		done:        make(chan struct{}),
		subs:        make(map[string]byte),
		pendingQoS1: make(map[uint16]mqttPendingPublish),
	}
	device.hubClient = &WSClient{
		DeviceID: clientID,
		UserID:   deviceUserID,
		Send:     make(chan []byte, wsSendQueueSize),
		Virtual:  true,
	}
	device.hubClient.Disconnect = device.close

	b.register(device)
	if b.hub != nil {
		if !b.hub.enqueueRegister(device.hubClient) {
			device.close()
			return
		}
	}
	defer func() {
		b.unregister(device)
		if b.hub != nil {
			b.hub.enqueueUnregister(device.hubClient)
		}
		device.close()
	}()

	go device.writeLoop()
	go device.bridgeHubMessages()
	go device.retryQoS1Loop()
	device.enqueue(makeMQTTConnack(0))

	timeout := 90 * time.Second
	if keepAlive > 0 {
		timeout = time.Duration(float64(keepAlive)*1.5)*time.Second + mqttKeepAliveGrace
	}

	for {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		packet, err := readMQTTPacket(conn)
		if err != nil {
			return
		}

		switch packet.header >> 4 {
		case 3:
			publish, err := parseMQTTPublish(packet.header, packet.body)
			if err != nil {
				return
			}
			if publish.qos == 1 {
				device.enqueue(makeMQTTPuback(publish.packetID))
			}
			b.handleDevicePublish(device, publish.topic, publish.payload)

		case 8:
			if packet.header&15 != 2 {
				return
			}
			packetID, subscriptions, err := parseMQTTSubscribe(packet.body)
			if err != nil {
				return
			}
			granted := make([]byte, 0, len(subscriptions))
			device.subMu.Lock()
			for _, subscription := range subscriptions {
				if !deviceTopicFilterAllowed(clientID, subscription.filter) {
					granted = append(granted, 0x80)
					continue
				}
				grantedQoS := subscription.qos
				if grantedQoS > 1 {
					grantedQoS = 1
				}
				device.subs[subscription.filter] = grantedQoS
				granted = append(granted, grantedQoS)
			}
			device.subMu.Unlock()
			device.enqueue(makeMQTTSuback(packetID, granted))

		case 12:
			if packet.header != 0xC0 || len(packet.body) != 0 {
				return
			}
			device.enqueue(writeMQTTPacket(0xD0, nil))

		case 14:
			return

		case 4:
			if packet.header != 0x40 || len(packet.body) != 2 {
				return
			}
			packetID := uint16(packet.body[0])<<8 | uint16(packet.body[1])
			device.packetMu.Lock()
			delete(device.pendingQoS1, packetID)
			device.packetMu.Unlock()

		case 5, 6:
			return

		default:
			log.Printf("unsupported MQTT packet from %s: 0x%02x", clientID, packet.header)
			return
		}
	}
}

func deviceTopicFilterAllowed(deviceID, filter string) bool {
	base := "cloud/device/" + deviceID + "/"
	return filter == base+"command" || filter == base+"event" || filter == base+"#"
}

func deviceTopicAllowed(deviceID, topic string) bool {
	base := "cloud/device/" + deviceID + "/"
	legacyBase := "device/" + deviceID + "/"
	validPrefix := strings.HasPrefix(topic, base) || strings.HasPrefix(topic, legacyBase)
	return validPrefix && (strings.HasSuffix(topic, "/event") || strings.HasSuffix(topic, "/status") || strings.HasSuffix(topic, "/ack"))
}

func (b *MQTTBroker) handleDevicePublish(device *mqttDevice, topic string, payload []byte) {
	if b.hub != nil && b.hub.db != nil && !deviceTopicAllowed(device.id, topic) {
		log.Printf("MQTT topic rejected from %s: %s", device.id, topic)
		return
	}
	// 业务消息沿用原 WebSocket 的 JSON 信封，直接交给现有业务处理器。
	var message WSMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		log.Printf("MQTT invalid JSON from %s: %v", device.id, err)
		return
	}
	if message.Type != "" && device.hubClient != nil {
		device.hubClient.handleMessage(message)
	}

	// 同时把设备事件转给其他 MQTT 订阅者，便于 ACK、监控和后续服务扩展。
	b.publish(topic, payload, device)
}

func (b *MQTTBroker) register(device *mqttDevice) {
	b.mu.Lock()
	old := b.clients[device.id]
	b.clients[device.id] = device
	b.mu.Unlock()
	if old != nil && old != device {
		old.close()
	}
	log.Printf("MQTT device connected: %s", device.id)
}

func (b *MQTTBroker) unregister(device *mqttDevice) {
	b.mu.Lock()
	if current, ok := b.clients[device.id]; ok && current == device {
		delete(b.clients, device.id)
	}
	b.mu.Unlock()
	log.Printf("MQTT device disconnected: %s", device.id)
}

func (b *MQTTBroker) publish(topic string, payload []byte, source *mqttDevice) {
	b.mu.RLock()
	clients := make([]*mqttDevice, 0, len(b.clients))
	for _, client := range b.clients {
		clients = append(clients, client)
	}
	b.mu.RUnlock()

	packet := makeMQTTPublish(topic, payload)
	for _, client := range clients {
		if source != nil && client == source {
			continue
		}
		client.subMu.RLock()
		matched := false
		for filter := range client.subs {
			if mqttTopicMatches(filter, topic) {
				matched = true
				break
			}
		}
		client.subMu.RUnlock()
		if matched {
			client.enqueue(packet)
		}
	}
}

func (b *MQTTBroker) deviceIDs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	ids := make([]string, 0, len(b.clients))
	for id := range b.clients {
		ids = append(ids, id)
	}
	return ids
}

func (c *mqttDevice) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *mqttDevice) enqueue(packet []byte) bool {
	select {
	case <-c.done:
		return false
	case c.send <- packet:
		return true
	default:
		c.close()
		return false
	}
}

func (c *mqttDevice) enqueueQoS1(topic string, payload []byte) bool {
	c.packetMu.Lock()
	if len(c.pendingQoS1) >= mqttMaxInflightQoS1 {
		c.packetMu.Unlock()
		c.close()
		return false
	}
	for {
		c.nextPacketID++
		if c.nextPacketID == 0 {
			c.nextPacketID = 1
		}
		if _, exists := c.pendingQoS1[c.nextPacketID]; !exists {
			break
		}
	}
	packetID := c.nextPacketID
	packet := makeMQTTPublishQoS1(topic, payload, packetID, false)
	c.pendingQoS1[packetID] = mqttPendingPublish{packet: packet, sentAt: time.Now(), attempts: 1}
	c.packetMu.Unlock()
	if c.enqueue(packet) {
		return true
	}
	c.packetMu.Lock()
	delete(c.pendingQoS1, packetID)
	c.packetMu.Unlock()
	return false
}

func (c *mqttDevice) retryQoS1Loop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case now := <-ticker.C:
			var retryPackets [][]byte
			shouldClose := false
			c.packetMu.Lock()
			for packetID, pending := range c.pendingQoS1 {
				if now.Sub(pending.sentAt) < mqttQoS1Retry {
					continue
				}
				if pending.attempts >= mqttQoS1MaxAttempts {
					shouldClose = true
					break
				}
				packet := append([]byte(nil), pending.packet...)
				packet[0] |= 0x08 // MQTT DUP flag
				pending.packet = packet
				pending.sentAt = now
				pending.attempts++
				c.pendingQoS1[packetID] = pending
				retryPackets = append(retryPackets, packet)
			}
			c.packetMu.Unlock()
			if shouldClose {
				// A peer that no longer acknowledges QoS1 is stale even if TCP still
				// looks open. Reconnect and the durable outbox will replay safely.
				c.close()
				return
			}
			for _, packet := range retryPackets {
				if !c.enqueue(packet) {
					return
				}
			}
		}
	}
}

func (c *mqttDevice) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case packet := <-c.send:
			if packet == nil {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := c.conn.Write(packet); err != nil {
				c.close()
				return
			}
		}
	}
}

// Stop 停止 broker listener，并让现有客户端自然退出。
func (b *MQTTBroker) Stop() {
	if b == nil {
		return
	}
	b.stopOnce.Do(func() {
		close(b.stop)
		b.mu.RLock()
		listener := b.listener
		clients := make([]*mqttDevice, 0, len(b.clients))
		for _, client := range b.clients {
			clients = append(clients, client)
		}
		b.mu.RUnlock()
		if listener != nil {
			_ = listener.Close()
		}
		for _, client := range clients {
			client.close()
		}
	})
}

// bridgeHubMessages 把原 WSHub 发给设备的 JSON 消息编码成 MQTT PUBLISH。
func (c *mqttDevice) bridgeHubMessages() {
	for {
		select {
		case <-c.done:
			return
		case data, ok := <-c.hubClient.Send:
			if !ok {
				return
			}
			topic := "cloud/device/" + c.id + "/command"
			c.enqueueQoS1(topic, data)
		}
	}
}

func readMQTTPacket(reader io.Reader) (mqttPacket, error) {
	var first [1]byte
	if _, err := io.ReadFull(reader, first[:]); err != nil {
		return mqttPacket{}, err
	}
	remaining, err := readMQTTRemainingLength(reader)
	if err != nil {
		return mqttPacket{}, err
	}
	if remaining > mqttMaxPacketSize {
		return mqttPacket{}, fmt.Errorf("mqtt packet too large: %d", remaining)
	}
	body := make([]byte, remaining)
	if _, err := io.ReadFull(reader, body); err != nil {
		return mqttPacket{}, err
	}
	return mqttPacket{header: first[0], body: body}, nil
}

func readMQTTRemainingLength(reader io.Reader) (int, error) {
	multiplier := 1
	value := 0
	for i := 0; i < 4; i++ {
		var one [1]byte
		if _, err := io.ReadFull(reader, one[:]); err != nil {
			return 0, err
		}
		value += int(one[0]&127) * multiplier
		if one[0]&128 == 0 {
			return value, nil
		}
		multiplier *= 128
	}
	return 0, errMQTTMalformed
}

func writeMQTTPacket(header byte, body []byte) []byte {
	packet := make([]byte, 1+mqttRemainingLengthSize(len(body))+len(body))
	packet[0] = header
	n := encodeMQTTRemainingLength(packet[1:], len(body))
	copy(packet[1+n:], body)
	return packet
}

func mqttRemainingLengthSize(value int) int {
	size := 1
	for value >= 128 {
		size++
		value /= 128
	}
	return size
}

func encodeMQTTRemainingLength(dst []byte, value int) int {
	index := 0
	for {
		digit := byte(value % 128)
		value /= 128
		if value > 0 {
			digit |= 128
		}
		dst[index] = digit
		index++
		if value == 0 {
			return index
		}
	}
}

func appendMQTTUTF8(dst []byte, value string) []byte {
	raw := []byte(value)
	if len(raw) > 65535 {
		raw = raw[:65535]
	}
	dst = append(dst, byte(len(raw)>>8), byte(len(raw)))
	return append(dst, raw...)
}

func readMQTTU16(body []byte, offset *int) (uint16, error) {
	if *offset+2 > len(body) {
		return 0, errMQTTMalformed
	}
	value := uint16(body[*offset])<<8 | uint16(body[*offset+1])
	*offset += 2
	return value, nil
}

func readMQTTUTF8(body []byte, offset *int) (string, error) {
	length, err := readMQTTU16(body, offset)
	if err != nil {
		return "", err
	}
	end := *offset + int(length)
	if end > len(body) {
		return "", errMQTTMalformed
	}
	value := string(body[*offset:end])
	*offset = end
	return value, nil
}

func readMQTTConnect(conn net.Conn) (string, uint16, error) {
	info, err := readMQTTConnectInfo(conn)
	if err != nil {
		return "", 0, err
	}
	return info.clientID, info.keepAlive, nil
}

type mqttConnectInfo struct {
	clientID    string
	keepAlive   uint16
	username    string
	password    string
	usernameSet bool
	passwordSet bool
}

func readMQTTConnectInfo(conn net.Conn) (mqttConnectInfo, error) {
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	packet, err := readMQTTPacket(conn)
	if err != nil {
		return mqttConnectInfo{}, err
	}
	if packet.header != 0x10 {
		return mqttConnectInfo{}, errMQTTMalformed
	}
	offset := 0
	protocolName, err := readMQTTUTF8(packet.body, &offset)
	if err != nil || protocolName != "MQTT" {
		return mqttConnectInfo{}, errMQTTMalformed
	}
	if offset+4 > len(packet.body) || packet.body[offset] != 4 {
		return mqttConnectInfo{}, errMQTTUnsupported
	}
	flags := packet.body[offset+1]
	if flags&1 != 0 {
		return mqttConnectInfo{}, errMQTTMalformed
	}
	keepAlive := uint16(packet.body[offset+2])<<8 | uint16(packet.body[offset+3])
	offset += 4
	clientID, err := readMQTTUTF8(packet.body, &offset)
	if err != nil || clientID == "" || len(clientID) > 128 {
		return mqttConnectInfo{}, errMQTTMalformed
	}
	willFlag := flags&0x04 != 0
	willQoS := (flags >> 3) & 0x03
	if (!willFlag && (willQoS != 0 || flags&0x20 != 0)) || willQoS == 3 {
		return mqttConnectInfo{}, errMQTTMalformed
	}
	if willFlag {
		if _, err := readMQTTUTF8(packet.body, &offset); err != nil {
			return mqttConnectInfo{}, err
		}
		if _, err := readMQTTUTF8(packet.body, &offset); err != nil {
			return mqttConnectInfo{}, err
		}
	}
	info := mqttConnectInfo{clientID: clientID, keepAlive: keepAlive}
	if flags&0x80 != 0 {
		info.username, err = readMQTTUTF8(packet.body, &offset)
		if err != nil {
			return mqttConnectInfo{}, err
		}
		info.usernameSet = true
	}
	if flags&0x40 != 0 {
		info.password, err = readMQTTUTF8(packet.body, &offset)
		if err != nil {
			return mqttConnectInfo{}, err
		}
		info.passwordSet = true
	}
	if info.passwordSet && !info.usernameSet {
		return mqttConnectInfo{}, errMQTTMalformed
	}
	if offset != len(packet.body) {
		return mqttConnectInfo{}, errMQTTMalformed
	}
	return info, nil
}

func parseMQTTPublish(header byte, body []byte) (mqttPublish, error) {
	qos := (header >> 1) & 3
	if qos == 3 {
		return mqttPublish{}, errMQTTMalformed
	}
	offset := 0
	topic, err := readMQTTUTF8(body, &offset)
	if err != nil || topic == "" || strings.ContainsAny(topic, "+#") {
		return mqttPublish{}, errMQTTMalformed
	}
	var packetID uint16
	if qos > 0 {
		packetID, err = readMQTTU16(body, &offset)
		if err != nil || packetID == 0 {
			return mqttPublish{}, errMQTTMalformed
		}
	}
	return mqttPublish{
		topic:    topic,
		payload:  append([]byte(nil), body[offset:]...),
		qos:      qos,
		packetID: packetID,
	}, nil
}

func parseMQTTSubscribe(body []byte) (uint16, []mqttSubscription, error) {
	offset := 0
	packetID, err := readMQTTU16(body, &offset)
	if err != nil || packetID == 0 {
		return 0, nil, errMQTTMalformed
	}
	var subscriptions []mqttSubscription
	for offset < len(body) {
		filter, err := readMQTTUTF8(body, &offset)
		if err != nil || filter == "" {
			return 0, nil, errMQTTMalformed
		}
		if offset >= len(body) {
			return 0, nil, errMQTTMalformed
		}
		qos := body[offset]
		offset++
		if qos > 2 {
			return 0, nil, errMQTTMalformed
		}
		subscriptions = append(subscriptions, mqttSubscription{filter: filter, qos: qos})
	}
	if len(subscriptions) == 0 {
		return 0, nil, errMQTTMalformed
	}
	return packetID, subscriptions, nil
}

func makeMQTTConnack(code byte) []byte {
	return writeMQTTPacket(0x20, []byte{0, code})
}

func makeMQTTSuback(packetID uint16, granted []byte) []byte {
	body := []byte{byte(packetID >> 8), byte(packetID)}
	body = append(body, granted...)
	return writeMQTTPacket(0x90, body)
}

func makeMQTTPuback(packetID uint16) []byte {
	return writeMQTTPacket(0x40, []byte{byte(packetID >> 8), byte(packetID)})
}

func makeMQTTPublish(topic string, payload []byte) []byte {
	body := appendMQTTUTF8(nil, topic)
	body = append(body, payload...)
	return writeMQTTPacket(0x30, body)
}

func makeMQTTPublishQoS1(topic string, payload []byte, packetID uint16, duplicate bool) []byte {
	body := appendMQTTUTF8(nil, topic)
	body = append(body, byte(packetID>>8), byte(packetID))
	body = append(body, payload...)
	header := byte(0x32)
	if duplicate {
		header |= 0x08
	}
	return writeMQTTPacket(header, body)
}

func mqttTopicMatches(filter, topic string) bool {
	filterParts := strings.Split(filter, "/")
	topicParts := strings.Split(topic, "/")
	for index, part := range filterParts {
		if part == "#" {
			return index == len(filterParts)-1
		}
		if index >= len(topicParts) {
			return false
		}
		if part != "+" && part != topicParts[index] {
			return false
		}
	}
	return len(filterParts) == len(topicParts)
}
