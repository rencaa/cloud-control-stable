package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"
)

const (
	deliveryQueued       = "queued"
	deliverySent         = "sent"
	deliveryAcknowledged = "acknowledged"
	deliveryFailed       = "failed"
)

// reliableDeliveryEnabled is intentionally a single opt-in gate. Keeping this
// false preserves the historical direct-send path byte-for-byte at runtime.
func reliableDeliveryEnabled() bool {
	return config.App != nil && config.App.Reliability.ReliableDeliveryEnabled
}

func reliableRetryDelay(attempts int) time.Duration {
	if config.App == nil || config.App.Reliability.RetrySeconds <= 0 {
		return 20 * time.Second
	}
	base := time.Duration(config.App.Reliability.RetrySeconds) * time.Second
	maximum := 5 * time.Minute
	if config.App.Reliability.MaxRetrySeconds > 0 {
		maximum = time.Duration(config.App.Reliability.MaxRetrySeconds) * time.Second
	}
	// Exponential backoff avoids continuously rewriting SQLite while a phone is
	// offline for hours. A reconnect wake-up bypasses this delay immediately.
	for step := 0; step < attempts && step < 12 && base < maximum; step++ {
		base *= 2
		if base >= maximum {
			return maximum
		}
	}
	return base
}

func reliableDeliveryTTL() time.Duration {
	if config.App == nil || config.App.Reliability.DeliveryTTLHours <= 0 {
		return 7 * 24 * time.Hour
	}
	return time.Duration(config.App.Reliability.DeliveryTTLHours) * time.Hour
}

func reliableMaxAttempts() int {
	if config.App == nil || config.App.Reliability.MaxAttempts <= 0 {
		return 5
	}
	return config.App.Reliability.MaxAttempts
}

func reliableRetryBatchSize() int {
	if config.App == nil || config.App.Reliability.RetryBatchSize <= 0 {
		return 100
	}
	return config.App.Reliability.RetryBatchSize
}

func commandIDFromMessage(message WSMessage) string {
	// WSMessage.Data is often gin.H, which is a named map type. Round-tripping
	// this tiny envelope keeps the helper independent of that concrete type.
	raw, err := json.Marshal(message.Data)
	if err != nil {
		return ""
	}
	data := make(map[string]interface{})
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	commandID, _ := data["cmd_id"].(string)
	return strings.TrimSpace(commandID)
}

func stringFieldFromMessage(message WSMessage, field string) string {
	raw, err := json.Marshal(message.Data)
	if err != nil {
		return ""
	}
	data := make(map[string]interface{})
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	value, _ := data[field].(string)
	return strings.TrimSpace(value)
}

// SendCommandWithDelivery persists an opt-in command before the first send.
// The old direct socket call remains the fallback when the feature is disabled
// or when a message has no cmd_id and therefore cannot be acknowledged safely.
func (h *WSHub) SendCommandWithDelivery(deviceID string, message WSMessage, taskID uint64) error {
	if !reliableDeliveryEnabled() {
		return h.SendToDevice(deviceID, message)
	}
	if h == nil || h.db == nil {
		return fmt.Errorf("reliable delivery database unavailable")
	}
	commandID := commandIDFromMessage(message)
	if commandID == "" {
		return h.SendToDevice(deviceID, message)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	now := time.Now()
	delivery := models.CommandDelivery{
		CommandID:   commandID,
		DeviceID:    deviceID,
		TaskID:      taskID,
		RunID:       stringFieldFromMessage(message, "run_id"),
		MessageType: message.Type,
		Payload:     string(payload),
		Status:      deliveryQueued,
		NextRetryAt: now,
	}
	if taskID > 0 && delivery.RunID != "" {
		// A recurring task represents current desired work, not an unbounded
		// historical queue. Keep only the newest run for each offline device.
		if err := h.db.Model(&models.CommandDelivery{}).
			Where("task_id = ? AND device_id = ? AND run_id <> ? AND status IN ?", taskID, deviceID, delivery.RunID, []string{deliveryQueued, deliverySent}).
			Updates(map[string]interface{}{"status": deliveryFailed, "last_error": "superseded by newer task run"}).Error; err != nil {
			return fmt.Errorf("supersede older delivery: %w", err)
		}
	}
	if err := h.db.Create(&delivery).Error; err != nil {
		return fmt.Errorf("persist command delivery: %w", err)
	}
	return h.attemptCommandDelivery(&delivery, now)
}

// SendCommandsWithDelivery keeps the existing batch-send API available while
// making every device command durable when the feature is explicitly enabled.
func (h *WSHub) SendCommandsWithDelivery(deviceIDs []string, message WSMessage, taskID uint64) (BatchSendResult, error) {
	if !reliableDeliveryEnabled() {
		return h.SendToDevices(deviceIDs, message)
	}
	result := BatchSendResult{}
	seen := make(map[string]struct{}, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		if _, exists := seen[deviceID]; exists {
			continue
		}
		seen[deviceID] = struct{}{}
		err := h.SendCommandWithDelivery(deviceID, message, taskID)
		switch {
		case err == nil:
			result.Sent++
		case err == ErrDeviceOffline:
			result.Offline++
		case err == ErrSendQueueFull:
			result.QueueFull++
		default:
			return result, err
		}
	}
	return result, nil
}

func (h *WSHub) attemptCommandDelivery(delivery *models.CommandDelivery, now time.Time) error {
	if h == nil || h.db == nil || delivery == nil {
		return fmt.Errorf("reliable delivery unavailable")
	}
	if delivery.Attempts >= reliableMaxAttempts() {
		return h.db.Model(&models.CommandDelivery{}).Where("id = ?", delivery.ID).Updates(map[string]interface{}{
			"status": deliveryFailed, "last_error": "retry limit reached",
		}).Error
	}
	if !delivery.CreatedAt.IsZero() && now.Sub(delivery.CreatedAt) > reliableDeliveryTTL() {
		return h.db.Model(&models.CommandDelivery{}).Where("id = ?", delivery.ID).Updates(map[string]interface{}{
			"status": deliveryFailed, "last_error": "delivery TTL expired",
		}).Error
	}

	var message WSMessage
	if err := json.Unmarshal([]byte(delivery.Payload), &message); err != nil {
		return h.db.Model(&models.CommandDelivery{}).Where("id = ?", delivery.ID).Updates(map[string]interface{}{
			"status": deliveryFailed, "last_error": "invalid persisted payload",
		}).Error
	}

	nextRetry := now.Add(reliableRetryDelay(delivery.Attempts))
	updates := map[string]interface{}{
		"attempts":      delivery.Attempts + 1,
		"next_retry_at": nextRetry,
	}
	if err := h.SendToDevice(delivery.DeviceID, message); err != nil {
		updates["status"] = deliveryQueued
		updates["last_error"] = truncateDeliveryError(err.Error())
		if dbErr := h.db.Model(&models.CommandDelivery{}).Where("id = ? AND status IN ?", delivery.ID, []string{deliveryQueued, deliverySent}).Updates(updates).Error; dbErr != nil {
			return dbErr
		}
		return err
	}
	updates["status"] = deliverySent
	updates["last_error"] = ""
	if err := h.db.Model(&models.CommandDelivery{}).Where("id = ? AND status IN ?", delivery.ID, []string{deliveryQueued, deliverySent}).Updates(updates).Error; err != nil {
		return err
	}
	if delivery.TaskID > 0 {
		query := h.db.Model(&models.TaskDevice{}).
			Where("task_id = ? AND device_id = (SELECT id FROM devices WHERE device_id = ?)", delivery.TaskID, delivery.DeviceID)
		if delivery.RunID != "" {
			query = query.Where("run_id = ?", delivery.RunID)
		}
		_ = query.
			Updates(map[string]interface{}{"status": 1, "started_at": now}).Error
	}
	return nil
}

func truncateDeliveryError(value string) string {
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func (h *WSHub) retryPendingDeliveries() {
	if !reliableDeliveryEnabled() || h == nil || h.db == nil {
		return
	}
	now := time.Now()
	var deliveries []models.CommandDelivery
	cutoff := now.Add(-reliableDeliveryTTL())
	if err := h.db.Where("status IN ? AND next_retry_at <= ? AND attempts < ? AND created_at >= ?", []string{deliveryQueued, deliverySent}, now, reliableMaxAttempts(), cutoff).
		Order("id ASC").Limit(reliableRetryBatchSize()).Find(&deliveries).Error; err != nil {
		log.Printf("Reliable delivery retry query failed: %v", err)
		return
	}
	for index := range deliveries {
		if err := h.attemptCommandDelivery(&deliveries[index], now); err != nil {
			log.Printf("Reliable delivery retry failed command=%s device=%s: %v", deliveries[index].CommandID, deliveries[index].DeviceID, err)
		}
	}
	// Mark records that exhausted their retry budget in a separate update so
	// they are observable instead of remaining indefinitely in the outbox.
	if err := h.db.Model(&models.CommandDelivery{}).
		Where("status IN ? AND attempts >= ?", []string{deliveryQueued, deliverySent}, reliableMaxAttempts()).
		Updates(map[string]interface{}{"status": deliveryFailed, "last_error": "retry limit reached"}).Error; err != nil {
		log.Printf("Reliable delivery exhaustion update failed: %v", err)
	}
	if err := h.db.Model(&models.CommandDelivery{}).
		Where("status IN ? AND created_at < ?", []string{deliveryQueued, deliverySent}, cutoff).
		Updates(map[string]interface{}{"status": deliveryFailed, "last_error": "delivery TTL expired"}).Error; err != nil {
		log.Printf("Reliable delivery TTL update failed: %v", err)
	}
}

func (h *WSHub) retryDeviceDeliveries(deviceID string) {
	if !reliableDeliveryEnabled() || h == nil || h.db == nil || strings.TrimSpace(deviceID) == "" {
		return
	}
	now := time.Now()
	var deliveries []models.CommandDelivery
	if err := h.db.Where("device_id = ? AND status IN ? AND attempts < ? AND created_at >= ?", deviceID,
		[]string{deliveryQueued, deliverySent}, reliableMaxAttempts(), now.Add(-reliableDeliveryTTL())).
		Order("id ASC").Limit(reliableRetryBatchSize()).Find(&deliveries).Error; err != nil {
		log.Printf("Reliable delivery reconnect query failed device=%s: %v", deviceID, err)
		return
	}
	for index := range deliveries {
		if err := h.attemptCommandDelivery(&deliveries[index], now); err != nil && err != ErrDeviceOffline {
			log.Printf("Reliable reconnect delivery failed command=%s device=%s: %v", deliveries[index].CommandID, deviceID, err)
		}
	}
}

// WakeDeviceDeliveries makes reconnect delivery immediate without launching a
// goroutine per connection or blocking the Hub registration loop.
func (h *WSHub) WakeDeviceDeliveries(deviceID string) {
	if !reliableDeliveryEnabled() || h == nil || strings.TrimSpace(deviceID) == "" {
		return
	}
	select {
	case h.deliveryWake <- deviceID:
	default:
		// The one-second periodic scan remains the bounded fallback.
	}
}

func (h *WSHub) runReliableDelivery() {
	if !reliableDeliveryEnabled() {
		return
	}
	log.Printf("Reliable command delivery enabled: retry=%s..%s max_attempts=%d ttl=%s", reliableRetryDelay(0), reliableRetryDelay(64), reliableMaxAttempts(), reliableDeliveryTTL())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.stop:
			return
		case deviceID := <-h.deliveryWake:
			h.retryDeviceDeliveries(deviceID)
		case <-ticker.C:
			h.retryPendingDeliveries()
		}
	}
}

// AcknowledgeCommandDelivery is called directly from the device read loop. An
// ACK is intentionally persisted synchronously: dropping it would turn a
// successfully received command into an unnecessary retry.
func (h *WSHub) AcknowledgeCommandDelivery(deviceID, commandID string, ok bool, message string) {
	if !reliableDeliveryEnabled() || h == nil || h.db == nil || strings.TrimSpace(commandID) == "" {
		return
	}
	now := time.Now()
	updates := map[string]interface{}{"acknowledged_at": now, "last_error": ""}
	if ok {
		updates["status"] = deliveryAcknowledged
	} else {
		updates["status"] = deliveryFailed
		updates["last_error"] = truncateDeliveryError(message)
	}
	if err := h.db.Model(&models.CommandDelivery{}).
		Where("command_id = ? AND device_id = ? AND status IN ?", commandID, deviceID, []string{deliveryQueued, deliverySent}).
		Updates(updates).Error; err != nil {
		log.Printf("Reliable delivery ACK persistence failed command=%s device=%s: %v", commandID, deviceID, err)
	}
}
