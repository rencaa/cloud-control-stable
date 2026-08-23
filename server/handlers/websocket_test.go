package handlers

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

func TestRequestUsesTLSRejectsUntrustedForwardedHeader(t *testing.T) {
	previousConfig := config.App
	config.App = &config.Config{Security: config.SecurityConfig{
		TrustedProxyCIDRs: []string{"127.0.0.1/32", "::1/128", "172.16.0.0/12"},
	}}
	t.Cleanup(func() { config.App = previousConfig })

	direct := httptest.NewRequest("GET", "http://example.test/ws", nil)
	direct.RemoteAddr = "203.0.113.12:49999"
	direct.Header.Set("X-Forwarded-Proto", "https")
	if requestUsesTLS(direct) {
		t.Fatal("public client spoofed X-Forwarded-Proto as trusted TLS")
	}

	localProxy := httptest.NewRequest("GET", "http://example.test/ws", nil)
	localProxy.RemoteAddr = "127.0.0.1:49999"
	localProxy.Header.Set("X-Forwarded-Proto", "https")
	if !requestUsesTLS(localProxy) {
		t.Fatal("local trusted reverse proxy HTTPS marker was rejected")
	}

	dockerProxy := httptest.NewRequest("GET", "http://example.test/ws", nil)
	dockerProxy.RemoteAddr = "172.20.0.9:49999"
	dockerProxy.Header.Set("X-Forwarded-Proto", "https")
	if !requestUsesTLS(dockerProxy) {
		t.Fatal("configured Docker proxy HTTPS marker was rejected")
	}

	nativeTLS := httptest.NewRequest("GET", "https://example.test/ws", nil)
	nativeTLS.TLS = &tls.ConnectionState{}
	if !requestUsesTLS(nativeTLS) {
		t.Fatal("native TLS request was rejected")
	}
}

func newTestHub(deviceCount, queueSize int) *WSHub {
	hub := &WSHub{clients: make(map[string]*WSClient, deviceCount)}
	for i := 0; i < deviceCount; i++ {
		id := fmt.Sprintf("device-%d", i)
		hub.clients[id] = &WSClient{DeviceID: id, Send: make(chan []byte, queueSize)}
	}
	return hub
}

func TestSendToDeviceReportsDeliveryState(t *testing.T) {
	hub := newTestHub(1, 1)

	if err := hub.SendToDevice("missing", WSMessage{Type: "command"}); !errors.Is(err, ErrDeviceOffline) {
		t.Fatalf("expected ErrDeviceOffline, got %v", err)
	}
	if err := hub.SendToDevice("device-0", WSMessage{Type: "command"}); err != nil {
		t.Fatalf("first send failed: %v", err)
	}
	if err := hub.SendToDevice("device-0", WSMessage{Type: "command"}); !errors.Is(err, ErrSendQueueFull) {
		t.Fatalf("expected ErrSendQueueFull, got %v", err)
	}
}

func TestExternalMQTTOnlineStateExpiresForFastWebSocketFallback(t *testing.T) {
	hub := newTestHub(0, 1)
	hub.TouchExternalDevice("external-device")
	if !hub.IsMQTTDevice("external-device") {
		t.Fatal("recent external MQTT heartbeat was not online")
	}
	hub.externalSeen.Store("external-device", time.Now().Add(-externalMQTTOnlineTTL-time.Second))
	if hub.IsMQTTDevice("external-device") {
		t.Fatal("stale external MQTT state blocked WebSocket fallback")
	}
}

func TestScreenshotQuotaDropsNewDataAndCleanupReleasesSpace(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "screenshots")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(directory, "old.jpg")
	if err := os.WriteFile(oldFile, []byte("12345678"), 0600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	previousConfig := config.App
	config.App = &config.Config{
		Upload:      config.UploadConfig{UploadPath: filepath.Dir(directory)},
		Maintenance: config.MaintenanceConfig{CleanupBatchSize: 10, ScreenshotMaxBytes: 10},
	}
	screenshotDiskState.Lock()
	screenshotDiskState.initialized = false
	screenshotDiskState.bytes = 0
	screenshotDiskState.pending = 0
	screenshotDiskState.Unlock()
	t.Cleanup(func() {
		config.App = previousConfig
		screenshotDiskState.Lock()
		screenshotDiskState.initialized = false
		screenshotDiskState.bytes = 0
		screenshotDiskState.pending = 0
		screenshotDiskState.Unlock()
	})

	if reserveScreenshotBytes(directory, 3) {
		t.Fatal("screenshot allocation exceeded quota")
	}
	cleanOldScreenshots(1, 10)
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expired screenshot was not removed: %v", err)
	}
	if !reserveScreenshotBytes(directory, 3) {
		t.Fatal("cleanup did not release screenshot quota")
	}
	temporaryPath := filepath.Join(directory, "new.jpg.tmp")
	finalPath := filepath.Join(directory, "new.jpg")
	if err := os.WriteFile(temporaryPath, []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishScreenshotFile(temporaryPath, finalPath, 3); err != nil {
		t.Fatal(err)
	}
	screenshotDiskState.Lock()
	bytes, pending := screenshotDiskState.bytes, screenshotDiskState.pending
	screenshotDiskState.Unlock()
	if bytes != 3 || pending != 0 {
		t.Fatalf("published screenshot accounting is bytes=%d pending=%d", bytes, pending)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("published screenshot is missing: %v", err)
	}
}

func TestVirtualClientCloseDoesNotCloseSendChannel(t *testing.T) {
	client := &WSClient{
		DeviceID: "mqtt-device",
		Send:     make(chan []byte, 1),
		Virtual:  true,
	}

	client.closeSend()

	select {
	case client.Send <- []byte(`{"type":"heartbeat"}`):
	default:
		t.Fatal("virtual MQTT client Send channel was closed or unavailable")
	}
}

func TestIsMQTTDevice(t *testing.T) {
	hub := &WSHub{clients: map[string]*WSClient{
		"mqtt-device": {DeviceID: "mqtt-device", Virtual: true},
		"ws-device":   {DeviceID: "ws-device"},
	}}

	if !hub.IsMQTTDevice("mqtt-device") {
		t.Fatal("expected MQTT device to be detected")
	}
	if hub.IsMQTTDevice("ws-device") {
		t.Fatal("WebSocket device must not be reported as MQTT")
	}
	if hub.IsMQTTDevice("missing") {
		t.Fatal("offline device must not be reported as MQTT")
	}
}

func TestDisconnectUserDevicesOnlyClosesOwnedTransports(t *testing.T) {
	closed := 0
	hub := &WSHub{clients: map[string]*WSClient{
		"owned": {DeviceID: "owned", UserID: 7, Send: make(chan []byte, 1), Disconnect: func() { closed++ }},
		"other": {DeviceID: "other", UserID: 8, Send: make(chan []byte, 1), Disconnect: func() { closed += 100 }},
	}}

	hub.DisconnectUserDevices(7)
	if closed != 1 {
		t.Fatalf("unexpected disconnected transport count: %d", closed)
	}
	if _, open := <-hub.clients["owned"].Send; open {
		t.Fatal("owned WebSocket send queue remained open")
	}
	select {
	case hub.clients["other"].Send <- []byte("still-open"):
	default:
		t.Fatal("another user's transport was closed")
	}
}

func TestSendToDevices(t *testing.T) {
	hub := newTestHub(3, 1)
	ids := []string{"device-0", "device-1", "device-2", "offline"}

	result, err := hub.SendToDevices(ids, WSMessage{Type: "command"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 3 || result.Offline != 1 || result.QueueFull != 0 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
}

func TestFlushHeartbeats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Device{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	device := models.Device{
		DeviceID:      "heartbeat-device",
		Name:          "heartbeat-device",
		Status:        1,
		RegisterAt:    now,
		LastHeartbeat: &now,
	}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}

	entry := heartbeatEntry{
		DeviceID:  device.DeviceID,
		Status:    3,
		IP:        "10.0.0.8",
		Battery:   88,
		UpdatedAt: now.Add(time.Second),
	}
	storeHeartbeat(entry)
	t.Cleanup(func() {
		heartbeatCache.Delete(device.DeviceID)
		heartbeatDirty.Delete(device.DeviceID)
	})

	flushHeartbeats(db)

	var got models.Device
	if err := db.Where("device_id = ?", device.DeviceID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != 3 || got.IP != entry.IP {
		t.Fatalf("heartbeat was not flushed: status=%d ip=%q", got.Status, got.IP)
	}
	if _, dirty := heartbeatDirty.Load(device.DeviceID); dirty {
		t.Fatal("dirty heartbeat was not cleared")
	}
}

func TestMergeHeartbeatKeepsFreshRunningStateFromOtherInstance(t *testing.T) {
	now := time.Now()
	current := heartbeatEntry{
		DeviceID:      "device-state",
		Status:        3,
		ScriptStatus:  "running",
		StateInstance: "business-instance",
		StateSeq:      2,
		LastBusyAt:    now,
		UpdatedAt:     now,
	}
	incoming := heartbeatEntry{
		DeviceID:      current.DeviceID,
		Status:        1,
		ScriptStatus:  "idle",
		StateInstance: "idle-agent-instance",
		StateSeq:      1,
		UpdatedAt:     now.Add(10 * time.Second),
	}

	got := mergeHeartbeat(current, incoming)
	if got.Status != 3 || got.ScriptStatus != "running" {
		t.Fatalf("fresh running state was overwritten: %+v", got)
	}
}

func TestMergeHeartbeatAcceptsExplicitOwnerStop(t *testing.T) {
	now := time.Now()
	current := heartbeatEntry{
		DeviceID:      "device-state",
		Status:        3,
		ScriptStatus:  "running",
		StateInstance: "business-instance",
		StateSeq:      2,
		LastBusyAt:    now,
		UpdatedAt:     now,
	}
	incoming := heartbeatEntry{
		DeviceID:        current.DeviceID,
		Status:          1,
		ScriptStatus:    "idle",
		StateInstance:   current.StateInstance,
		StateSeq:        3,
		StateTransition: true,
		UpdatedAt:       now.Add(time.Second),
	}

	got := mergeHeartbeat(current, incoming)
	if got.Status != 1 || got.ScriptStatus != "idle" {
		t.Fatalf("explicit stop was not accepted: %+v", got)
	}
}

func TestMergeHeartbeatRestoresBusyStatusAfterReconnect(t *testing.T) {
	now := time.Now()
	current := heartbeatEntry{
		DeviceID:      "device-state",
		Status:        0,
		ScriptStatus:  "running",
		StateInstance: "business-instance",
		LastBusyAt:    now,
		UpdatedAt:     now,
	}
	incoming := heartbeatEntry{
		DeviceID:  current.DeviceID,
		Status:    1,
		UpdatedAt: now.Add(time.Second),
	}

	got := mergeHeartbeat(current, incoming)
	if got.Status != 3 || got.ScriptStatus != "running" {
		t.Fatalf("busy state was not restored after reconnect: %+v", got)
	}
}

func TestNormalizeDeviceLogsSupportsHeartbeatAndBatchPayloads(t *testing.T) {
	raw := map[string]interface{}{
		"entries": []interface{}{
			"plain log",
			map[string]interface{}{"level": "warning", "message": "warn log"},
			map[string]interface{}{"log_type": "error", "msg": "error log"},
		},
	}

	got := normalizeDeviceLogs(raw)
	if len(got) != 3 {
		t.Fatalf("expected 3 logs, got %d: %+v", len(got), got)
	}
	if got[0].LogType != "info" ||
		got[1].LogType != "warn" ||
		got[2].LogType != "error" {
		t.Fatalf("unexpected log types: %+v", got)
	}
}

func TestDeviceLogsCreatePlaceholderWhenRegistrationIsStillPending(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Device{}, &models.DeviceLog{}); err != nil {
		t.Fatal(err)
	}

	previousHub := Hub
	hub := NewWSHub(db)
	go hub.Run()
	t.Cleanup(func() {
		hub.Stop()
		Hub = previousHub
		deviceMetaCache.Delete("log-before-register-device")
	})

	enqueueDeviceLogs("log-before-register-device", map[string]interface{}{
		"entries": []interface{}{
			map[string]interface{}{"level": "info", "message": "first log"},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		var count int64
		db.Model(&models.DeviceLog{}).Count(&count)
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first log was not persisted while device registration was pending")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var device models.Device
	if err := db.Where("device_id = ?", "log-before-register-device").First(&device).Error; err != nil {
		t.Fatalf("placeholder device was not created: %v", err)
	}
	if device.Name != device.DeviceID {
		t.Fatalf("unexpected placeholder device: %+v", device)
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename("../device/一号"); got != "device" {
		t.Fatalf("unexpected sanitized filename %q", got)
	}
}

func TestResolveDouyinRoomIDFromEmbeddedPayload(t *testing.T) {
	payload := []byte(`{"roomId":"7666620123230292786","web_rid":"449137376404"}`)
	if got := douyinRoomIDPattern.FindStringSubmatch(string(payload)); len(got) != 2 || got[1] != "7666620123230292786" {
		t.Fatalf("unexpected roomId extraction: %q", got)
	}
	escapedPayload := []byte(`{\"roomId\":\"7666620123230292786\",\"web_rid\":\"449137376404\"}`)
	if got := douyinRoomIDPattern.FindStringSubmatch(string(escapedPayload)); len(got) != 2 || got[1] != "7666620123230292786" {
		t.Fatalf("unexpected escaped roomId extraction: %q", got)
	}
}

func TestZeroTouchWebSocketRegistrationRequiresRegisterAndConfirmsToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := db.AutoMigrate(&models.User{}, &models.Device{}, &models.DeviceAutoRegistration{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Username: "ws-owner", Password: "hashed", Status: 1}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	previousHub, previousConfig := Hub, config.App
	config.App = &config.Config{Security: config.SecurityConfig{DeviceAuthRequired: true, DeviceAutoRegister: true, DeviceAutoRegisterRateLimit: 10}}
	hub := NewWSHub(db)
	go hub.Run()
	t.Cleanup(func() {
		hub.Stop()
		Hub = previousHub
		config.App = previousConfig
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ws/device/:device_id", DeviceWSHandler)
	server := httptest.NewServer(router)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	blocked, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws/device/blocked-device", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := blocked.WriteJSON(WSMessage{Type: "heartbeat", Data: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}
	_ = blocked.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := blocked.ReadMessage(); err == nil {
		t.Fatal("unregistered device remained open after sending a business message")
	}
	_ = blocked.Close()
	var blockedCount int64
	db.Model(&models.Device{}).Where("device_id = ?", "blocked-device").Count(&blockedCount)
	if blockedCount != 0 {
		t.Fatal("business message before registration created a device")
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws/device/zero-touch-device", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(WSMessage{Type: "register", Data: map[string]interface{}{"name": "Zero Touch"}}); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var response WSMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	data, ok := response.Data.(map[string]interface{})
	if !ok || response.Type != "register" {
		t.Fatalf("unexpected registration response: %+v", response)
	}
	token, _ := data["device_token"].(string)
	if token == "" {
		t.Fatalf("auto-registration response did not contain a token: %+v", response)
	}
	if err := conn.WriteJSON(WSMessage{Type: "register_ack"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		var audit models.DeviceAutoRegistration
		err := db.Where("device_id = ?", "zero-touch-device").First(&audit).Error
		if err == nil && audit.ConfirmedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auto-registration confirmation not persisted: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := AuthenticateDevice(db, "zero-touch-device", token); !ok {
		t.Fatal("issued zero-touch token cannot authenticate device")
	}
}

func TestLANNoAuthWebSocketCreatesDeviceWithoutToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := db.AutoMigrate(&models.Device{}); err != nil {
		t.Fatal(err)
	}
	previousHub, previousConfig := Hub, config.App
	config.App = &config.Config{Security: config.SecurityConfig{DeviceAuthRequired: false}}
	hub := NewWSHub(db)
	go hub.Run()
	t.Cleanup(func() {
		hub.Stop()
		Hub = previousHub
		config.App = previousConfig
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ws/device/:device_id", DeviceWSHandler)
	server := httptest.NewServer(router)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws/device/lan-phone-01", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(WSMessage{Type: "register", Data: map[string]interface{}{"name": "LAN Phone"}}); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response["type"] != "register" || response["message"] != "ok" {
		t.Fatalf("unexpected LAN registration response: %s", raw)
	}
	if data, ok := response["data"].(map[string]interface{}); ok && data["device_token"] != nil {
		t.Fatalf("LAN no-auth response unexpectedly issued a token: %s", raw)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		var device models.Device
		err := db.Where("device_id = ?", "lan-phone-01").First(&device).Error
		if err == nil {
			if device.Name != "LAN Phone" || device.AuthTokenHash != "" {
				t.Fatalf("unexpected LAN device: %+v", device)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("LAN device was not persisted: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebSocketSourceLimiterCapsActiveConnectionsAndReleases(t *testing.T) {
	previousConfig := config.App
	config.App = &config.Config{Security: config.SecurityConfig{
		DeviceWSAttemptsPerMinute:   10,
		DeviceWSMaxConnectionsPerIP: 1,
	}}
	wsSourceLimiter.Lock()
	wsSourceLimiter.entries = make(map[string]*wsSourceEntry)
	wsSourceLimiter.lastSweep = time.Time{}
	wsSourceLimiter.Unlock()
	t.Cleanup(func() {
		config.App = previousConfig
		wsSourceLimiter.Lock()
		wsSourceLimiter.entries = make(map[string]*wsSourceEntry)
		wsSourceLimiter.lastSweep = time.Time{}
		wsSourceLimiter.Unlock()
	})

	release, ok := acquireWSConnection("192.168.1.20")
	if !ok || release == nil {
		t.Fatal("first LAN connection was unexpectedly rejected")
	}
	if secondRelease, secondOK := acquireWSConnection("192.168.1.20"); secondOK || secondRelease != nil {
		t.Fatal("second simultaneous connection from the same source was accepted")
	}
	release()
	if nextRelease, nextOK := acquireWSConnection("192.168.1.20"); !nextOK || nextRelease == nil {
		t.Fatal("source slot was not released after disconnect")
	} else {
		nextRelease()
	}
}

func BenchmarkSendToDevices5000(b *testing.B) {
	hub := newTestHub(5000, b.N+1)
	ids := make([]string, 5000)
	for i := range ids {
		ids[i] = fmt.Sprintf("device-%d", i)
	}
	message := WSMessage{Type: "command", Data: map[string]interface{}{"command": "home"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := hub.SendToDevices(ids, message); err != nil {
			b.Fatal(err)
		}
	}
}
