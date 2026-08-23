package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	wsRegisterQueueSize   = 4096
	wsSendQueueSize       = 512
	dbJobQueueSize        = 1024
	dbWorkerCount         = 4
	mediaJobQueueSize     = 32
	mediaWorkerCount      = 2
	maxDBQueueBytes       = 64 * 1024 * 1024
	maxMediaQueueBytes    = 24 * 1024 * 1024
	maxScreenshotDataSize = 4 * 1024 * 1024
	maxResponseItems      = 200
	maxFrameCacheSize     = 32
	maxFrameCacheBytes    = 32 * 1024 * 1024
	maxFrameDataSize      = 2 * 1024 * 1024
	maxScreenWallDevices  = 16
	maxScreenResponseSize = 12 * 1024 * 1024
	externalMQTTOnlineTTL = 30 * time.Second
	maxWSMessageSize      = 8 * 1024 * 1024
	maxDeviceLogsBatch    = 100
	maxDeviceLogRunes     = 2048
	deviceBusyStateHold   = 25 * time.Second
)

var (
	ErrDeviceOffline       = errors.New("device offline")
	ErrSendQueueFull       = errors.New("device send queue full")
	notificationHTTPClient = &http.Client{Timeout: 5 * time.Second}
)

var wsWriteBufferPool sync.Pool

var upgrader = websocket.Upgrader{
	ReadBufferSize:    4096,
	WriteBufferSize:   4096,
	WriteBufferPool:   &wsWriteBufferPool,
	EnableCompression: false,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		if config.App == nil || len(config.App.Security.CORSOrigins) == 0 {
			return false
		}
		for _, allowed := range config.App.Security.CORSOrigins {
			if origin == allowed {
				return true
			}
		}
		return false
	},
}

// WSClient WebSocket 客户端连接
type WSClient struct {
	Conn                 *websocket.Conn
	DeviceID             string
	UserID               uint64
	AutoRegister         bool
	AutoRegisterRecovery bool
	SourceIP             string
	Send                 chan []byte
	Virtual              bool // MQTT 等非 WebSocket 连接没有 writePump，不能关闭 Send 通道
	Disconnect           func()
	registrationHandled  bool
	closeOnce            sync.Once
	sourceReleaseOnce    sync.Once
	ReleaseSource        func()
}

func (c *WSClient) closeSend() {
	c.closeOnce.Do(func() {
		// MQTT 虚拟客户端的业务处理仍可能并发写入 Send。
		// 它由 mqttDevice.done 负责结束 bridgeHubMessages，不能在这里
		// 关闭 channel，否则并发心跳/注册回执会触发 send on closed channel。
		if c.Virtual {
			return
		}
		close(c.Send)
	})
}

func (c *WSClient) closeTransport() {
	c.releaseSource()
	c.closeSend()
	if c.Conn != nil {
		_ = c.Conn.Close()
	}
	if c.Disconnect != nil {
		c.Disconnect()
	}
}

func (c *WSClient) releaseSource() {
	c.sourceReleaseOnce.Do(func() {
		if c.ReleaseSource != nil {
			c.ReleaseSource()
		}
	})
}

// WSHub WebSocket 中心
type WSHub struct {
	clients      map[string]*WSClient // device_id -> client
	register     chan *WSClient
	unregister   chan *WSClient
	dbJobs       chan weightedDBJob
	mediaJobs    chan weightedMediaJob
	deliveryWake chan string
	stop         chan struct{}
	stopOnce     sync.Once
	workerOnce   sync.Once
	workerWG     sync.WaitGroup
	stopping     atomic.Bool
	dbQueueBytes atomic.Int64
	mediaBytes   atomic.Int64
	mu           sync.RWMutex
	db           *gorm.DB
	externalMQTT *ExternalMQTTBridge
	externalSeen sync.Map
}

type weightedDBJob struct {
	bytes int64
	run   func(*gorm.DB)
}

type weightedMediaJob struct {
	bytes int64
	run   func()
}

var Hub *WSHub
var FrameCache = make(map[string]frameEntry)
var FrameMutex sync.RWMutex
var frameCacheBytes int
var screenshotDiskState = struct {
	sync.Mutex
	initialized bool
	bytes       int64
	pending     int64
}{}

// frameEntry 带时间戳的帧缓存
type frameEntry struct {
	Data      string
	UpdatedAt time.Time
}

// heartbeatCache 心跳内存缓存: device_id → heartbeatData
var heartbeatCache sync.Map

// heartbeatDirty 标记哪些设备的心跳数据已变更需要刷盘
var heartbeatDirty sync.Map

// heartbeatStateMu 保护同一设备多个连接/脚本实例并发上报时的状态合并。
// 手机可能同时存在“云控代理”和“业务脚本”两个实例，不能让空闲实例
// 覆盖仍在持续上报的运行实例。
var heartbeatStateMu sync.Mutex

// deviceMetaCache 避免任务状态、短信和日志上报时反复查询设备主键
var deviceMetaCache sync.Map

var autoRegisterLimiter = struct {
	sync.Mutex
	entries   map[string][]time.Time
	lastSweep time.Time
}{entries: make(map[string][]time.Time)}

type wsSourceEntry struct {
	attempts []time.Time
	active   int
	lastSeen time.Time
}

var wsSourceLimiter = struct {
	sync.Mutex
	entries   map[string]*wsSourceEntry
	lastSweep time.Time
}{entries: make(map[string]*wsSourceEntry)}

var deviceConnectionHistory = struct {
	sync.Mutex
	times []time.Time
}{}

func recordDeviceConnection(now time.Time) {
	cutoff := now.Add(-5 * time.Minute)
	deviceConnectionHistory.Lock()
	kept := deviceConnectionHistory.times[:0]
	for _, at := range deviceConnectionHistory.times {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	deviceConnectionHistory.times = append(kept, now)
	deviceConnectionHistory.Unlock()
}

func recentDeviceConnections(now time.Time) int {
	cutoff := now.Add(-5 * time.Minute)
	deviceConnectionHistory.Lock()
	kept := deviceConnectionHistory.times[:0]
	for _, at := range deviceConnectionHistory.times {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	deviceConnectionHistory.times = kept
	count := len(kept)
	deviceConnectionHistory.Unlock()
	return count
}

func acquireWSConnection(sourceIP string) (func(), bool) {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	attemptLimit, activeLimit := 120, 8
	if config.App != nil {
		if config.App.Security.DeviceWSAttemptsPerMinute > 0 {
			attemptLimit = config.App.Security.DeviceWSAttemptsPerMinute
		}
		if config.App.Security.DeviceWSMaxConnectionsPerIP > 0 {
			activeLimit = config.App.Security.DeviceWSMaxConnectionsPerIP
		}
	}
	sourceIP = strings.TrimSpace(sourceIP)
	if sourceIP == "" {
		sourceIP = "unknown"
	}
	wsSourceLimiter.Lock()
	if now.Sub(wsSourceLimiter.lastSweep) >= time.Minute {
		for key, entry := range wsSourceLimiter.entries {
			if entry.active == 0 && now.Sub(entry.lastSeen) > 2*time.Minute {
				delete(wsSourceLimiter.entries, key)
			}
		}
		wsSourceLimiter.lastSweep = now
	}
	entry := wsSourceLimiter.entries[sourceIP]
	if entry == nil {
		if len(wsSourceLimiter.entries) >= 4096 {
			wsSourceLimiter.Unlock()
			return nil, false
		}
		entry = &wsSourceEntry{}
		wsSourceLimiter.entries[sourceIP] = entry
	}
	kept := entry.attempts[:0]
	for _, at := range entry.attempts {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	entry.attempts = append(kept, now)
	entry.lastSeen = now
	if len(entry.attempts) > attemptLimit || entry.active >= activeLimit {
		wsSourceLimiter.Unlock()
		return nil, false
	}
	entry.active++
	wsSourceLimiter.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			wsSourceLimiter.Lock()
			if current := wsSourceLimiter.entries[sourceIP]; current != nil {
				if current.active > 0 {
					current.active--
				}
				current.lastSeen = time.Now()
			}
			wsSourceLimiter.Unlock()
		})
	}, true
}

var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// heartbeatEntry 心跳缓存条目
type heartbeatEntry struct {
	DeviceID        string
	Status          int8
	IP              string
	Province        string
	City            string
	ScriptStatus    string
	Battery         int
	OutboxDepth     int
	UpdatedAt       time.Time
	StateInstance   string
	StateSeq        int64
	StateTransition bool
	LastBusyAt      time.Time
}

type deviceMeta struct {
	ID   uint64
	Name string
}

// NewWSHub 创建WebSocket中心
func NewWSHub(db *gorm.DB) *WSHub {
	hub := &WSHub{
		clients:      make(map[string]*WSClient),
		register:     make(chan *WSClient, wsRegisterQueueSize),
		unregister:   make(chan *WSClient, wsRegisterQueueSize),
		dbJobs:       make(chan weightedDBJob, dbJobQueueSize),
		mediaJobs:    make(chan weightedMediaJob, mediaJobQueueSize),
		deliveryWake: make(chan string, 256),
		stop:         make(chan struct{}),
		db:           db,
	}
	Hub = hub
	return hub
}

// Run 启动WebSocket中心
func (h *WSHub) Run() {
	h.startWorkers()
	go h.runReliableDelivery()

	// 启动截图清理定时器
	go func() {
		maintenance := maintenanceSettings()
		cleanOldScreenshots(maintenance.ScreenshotRetentionHours, maintenance.ScreenshotMaxBytes)
		pruneRuntimeCaches(h)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-ticker.C:
				maintenance := maintenanceSettings()
				cleanOldScreenshots(maintenance.ScreenshotRetentionHours, maintenance.ScreenshotMaxBytes)
				pruneRuntimeCaches(h)
			}
		}
	}()
	// 启动心跳批量刷盘（每30秒将内存中的心跳数据写入DB）
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-ticker.C:
				flushHeartbeats(h.db)
			}
		}
	}()
	for {
		select {
		case <-h.stop:
			return
		case client := <-h.register:
			recordDeviceConnection(time.Now())
			var old *WSClient
			h.mu.Lock()
			old = h.clients[client.DeviceID]
			h.clients[client.DeviceID] = client
			if old != nil && old != client {
				old.closeSend()
			}
			h.mu.Unlock()
			if old != nil && old != client {
				if old.Conn != nil {
					_ = old.Conn.Close()
				}
			}
			touchHeartbeat(client.DeviceID, 1)
			h.WakeDeviceDeliveries(client.DeviceID)

			log.Printf("Device connected: %s", client.DeviceID)

		case client := <-h.unregister:
			removed := false
			h.mu.Lock()
			if current, ok := h.clients[client.DeviceID]; ok && current == client {
				delete(h.clients, client.DeviceID)
				client.closeSend()
				removed = true
			}
			h.mu.Unlock()

			// 旧连接被新连接替换时，不允许旧连接把新连接标记为离线。
			if !removed {
				continue
			}

			// 清理该设备的帧缓存
			FrameMutex.Lock()
			if frame, exists := FrameCache[client.DeviceID]; exists {
				frameCacheBytes -= len(frame.Data)
			}
			delete(FrameCache, client.DeviceID)
			FrameMutex.Unlock()
			deviceMetaCache.Delete(client.DeviceID)

			// 下线状态也走批量刷盘，避免大量设备同时断线时阻塞Hub。
			touchHeartbeat(client.DeviceID, 0)

			log.Printf("Device disconnected: %s", client.DeviceID)
		}
	}
}

func (h *WSHub) startWorkers() {
	h.workerOnce.Do(func() {
		h.workerWG.Add(dbWorkerCount + mediaWorkerCount)
		for i := 0; i < dbWorkerCount; i++ {
			go func() { defer h.workerWG.Done(); h.runDBWorker() }()
		}
		for i := 0; i < mediaWorkerCount; i++ {
			go func() { defer h.workerWG.Done(); h.runMediaWorker() }()
		}
	})
}

func pruneRuntimeCaches(h *WSHub) {
	if h == nil {
		return
	}
	online := make(map[string]struct{})
	h.mu.RLock()
	for deviceID := range h.clients {
		online[deviceID] = struct{}{}
	}
	h.mu.RUnlock()
	now := time.Now()
	heartbeatCache.Range(func(key, value interface{}) bool {
		deviceID, idOK := key.(string)
		entry, entryOK := value.(heartbeatEntry)
		_, isOnline := online[deviceID]
		if idOK && entryOK && !isOnline && now.Sub(entry.UpdatedAt) > 24*time.Hour {
			heartbeatCache.Delete(key)
			heartbeatDirty.Delete(key)
			deviceMetaCache.Delete(key)
		}
		return true
	})
	h.externalSeen.Range(func(key, value interface{}) bool {
		if seenAt, ok := value.(time.Time); !ok || now.Sub(seenAt) > 2*externalMQTTOnlineTTL {
			h.externalSeen.Delete(key)
		}
		return true
	})
	FrameMutex.Lock()
	for deviceID, frame := range FrameCache {
		if now.Sub(frame.UpdatedAt) > 10*time.Second {
			frameCacheBytes -= len(frame.Data)
			delete(FrameCache, deviceID)
		}
	}
	if frameCacheBytes < 0 {
		frameCacheBytes = 0
	}
	FrameMutex.Unlock()
}

func (h *WSHub) runDBWorker() {
	for {
		select {
		case job := <-h.dbJobs:
			h.executeDBJob(job)
		case <-h.stop:
			for {
				select {
				case job := <-h.dbJobs:
					h.executeDBJob(job)
				default:
					return
				}
			}
		}
	}
}

func (h *WSHub) enqueueDB(job func(*gorm.DB)) bool {
	return h.enqueueDBWeighted(16*1024, job)
}

func (h *WSHub) enqueueDBWeighted(bytes int64, job func(*gorm.DB)) bool {
	if job == nil || h.stopping.Load() || !reserveQueueBytes(&h.dbQueueBytes, bytes, maxDBQueueBytes) {
		return false
	}
	queued := weightedDBJob{bytes: normalizedQueueBytes(bytes), run: job}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-h.stop:
		h.dbQueueBytes.Add(-queued.bytes)
		return false
	case h.dbJobs <- queued:
		return true
	case <-timer.C:
		h.dbQueueBytes.Add(-queued.bytes)
		return false
	}
}

func (h *WSHub) executeDBJob(job weightedDBJob) {
	defer h.dbQueueBytes.Add(-job.bytes)
	if job.run == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("DB worker panic: %v", r)
		}
	}()
	job.run(h.db)
}

func (h *WSHub) runMediaWorker() {
	for {
		select {
		case job := <-h.mediaJobs:
			h.executeMediaJob(job)
		case <-h.stop:
			for {
				select {
				case job := <-h.mediaJobs:
					h.executeMediaJob(job)
				default:
					return
				}
			}
		}
	}
}

func (h *WSHub) enqueueMedia(job func()) bool {
	return h.enqueueMediaWeighted(8*1024, job)
}

func (h *WSHub) enqueueMediaWeighted(bytes int64, job func()) bool {
	if job == nil || h.stopping.Load() || !reserveQueueBytes(&h.mediaBytes, bytes, maxMediaQueueBytes) {
		return false
	}
	queued := weightedMediaJob{bytes: normalizedQueueBytes(bytes), run: job}
	select {
	case <-h.stop:
		h.mediaBytes.Add(-queued.bytes)
		return false
	case h.mediaJobs <- queued:
		return true
	default:
		h.mediaBytes.Add(-queued.bytes)
		return false
	}
}

func (h *WSHub) executeMediaJob(job weightedMediaJob) {
	defer h.mediaBytes.Add(-job.bytes)
	if job.run == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Media worker panic: %v", r)
		}
	}()
	job.run()
}

func normalizedQueueBytes(bytes int64) int64 {
	if bytes < 1 {
		return 1
	}
	return bytes
}

func reserveQueueBytes(counter *atomic.Int64, bytes, limit int64) bool {
	bytes = normalizedQueueBytes(bytes)
	if bytes > limit {
		return false
	}
	for {
		current := counter.Load()
		if current > limit-bytes {
			return false
		}
		if counter.CompareAndSwap(current, current+bytes) {
			return true
		}
	}
}

// Stop 关闭 hub 的后台 worker 和现有设备连接。
func (h *WSHub) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	h.StopWithContext(ctx)
}

// StopWithContext stops accepting work, closes device transports and drains
// bounded queues until the deadline. Critical ACK/task state already bypasses
// the lossy media path and therefore gets a chance to reach SQLite.
func (h *WSHub) StopWithContext(ctx context.Context) {
	if h == nil {
		return
	}
	h.startWorkers()
	h.stopOnce.Do(func() {
		h.stopping.Store(true)
		h.mu.RLock()
		clients := make([]*WSClient, 0, len(h.clients))
		for _, client := range h.clients {
			clients = append(clients, client)
		}
		h.mu.RUnlock()
		for _, client := range clients {
			client.closeTransport()
		}
		flushHeartbeats(h.db)
		close(h.stop)
	})
	done := make(chan struct{})
	go func() {
		h.workerWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("Hub queue drain timed out: db_jobs=%d media_jobs=%d db_bytes=%d media_bytes=%d", len(h.dbJobs), len(h.mediaJobs), h.dbQueueBytes.Load(), h.mediaBytes.Load())
	}
}

// DisconnectUserDevices immediately revokes live device transports when the
// owning user is disabled. New connections are denied by AuthenticateDevice.
func (h *WSHub) DisconnectUserDevices(userID uint64) {
	if h == nil || userID == 0 {
		return
	}
	h.mu.RLock()
	clients := make([]*WSClient, 0)
	for _, client := range h.clients {
		if client.UserID == userID {
			clients = append(clients, client)
		}
	}
	h.mu.RUnlock()
	for _, client := range clients {
		client.closeTransport()
	}
}

func (h *WSHub) enqueueRegister(client *WSClient) bool {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-h.stop:
		return false
	case h.register <- client:
		return true
	case <-timer.C:
		return false
	}
}

func (h *WSHub) enqueueUnregister(client *WSClient) bool {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-h.stop:
		return false
	case h.unregister <- client:
		return true
	case <-timer.C:
		return false
	}
}

// cleanOldScreenshots 清理过期的截图文件
func cleanOldScreenshots(maxHours int, maxBytes int64) {
	dir := screenshotDirectory()
	screenshotDiskState.Lock()
	defer screenshotDiskState.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type screenshotFile struct {
		name string
		size int64
		mod  time.Time
	}
	files := make([]screenshotFile, 0, len(entries))
	var totalBytes int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".tmp") {
			if info.ModTime().Before(time.Now().Add(-time.Hour)) {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
			}
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".jpg") {
			continue
		}
		files = append(files, screenshotFile{name: entry.Name(), size: info.Size(), mod: info.ModTime()})
		totalBytes += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	cutoff := time.Now().Add(-time.Duration(maxHours) * time.Hour)
	removed := 0
	for _, file := range files {
		if removed >= 500 {
			break
		}
		if file.mod.Before(cutoff) || (maxBytes > 0 && totalBytes+screenshotDiskState.pending > maxBytes) {
			if os.Remove(filepath.Join(dir, file.name)) == nil {
				removed++
				totalBytes -= file.size
			}
		}
	}
	screenshotDiskState.initialized = true
	screenshotDiskState.bytes = totalBytes + screenshotDiskState.pending
}

func reserveScreenshotBytes(dir string, size int64) bool {
	if size <= 0 {
		return false
	}
	if _, _, writable := storageWatermark(dir, size); !writable {
		return false
	}
	limit := maintenanceSettings().ScreenshotMaxBytes
	screenshotDiskState.Lock()
	defer screenshotDiskState.Unlock()
	if !screenshotDiskState.initialized {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jpg") {
				continue
			}
			if info, err := entry.Info(); err == nil {
				screenshotDiskState.bytes += info.Size()
			}
		}
		screenshotDiskState.initialized = true
	}
	if limit > 0 && screenshotDiskState.bytes+size > limit {
		return false
	}
	screenshotDiskState.bytes += size
	screenshotDiskState.pending += size
	return true
}

func publishScreenshotFile(temporaryPath, finalPath string, size int64) error {
	screenshotDiskState.Lock()
	defer screenshotDiskState.Unlock()
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		screenshotDiskState.bytes -= size
		screenshotDiskState.pending -= size
		if screenshotDiskState.bytes < 0 {
			screenshotDiskState.bytes = 0
		}
		if screenshotDiskState.pending < 0 {
			screenshotDiskState.pending = 0
		}
		return err
	}
	screenshotDiskState.pending -= size
	if screenshotDiskState.pending < 0 {
		screenshotDiskState.pending = 0
	}
	return nil
}

func releaseScreenshotBytes(size int64) {
	screenshotDiskState.Lock()
	screenshotDiskState.bytes -= size
	screenshotDiskState.pending -= size
	if screenshotDiskState.bytes < 0 {
		screenshotDiskState.bytes = 0
	}
	if screenshotDiskState.pending < 0 {
		screenshotDiskState.pending = 0
	}
	screenshotDiskState.Unlock()
}

func screenshotDirectory() string {
	uploadPath := "uploads"
	if config.App != nil && config.App.Upload.UploadPath != "" {
		uploadPath = config.App.Upload.UploadPath
	}
	return filepath.Join(uploadPath, "screenshots")
}

func sanitizeFilename(value string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, value)
	clean = strings.Trim(clean, ".-_")
	if clean == "" {
		return "device"
	}
	return clean
}

func scriptStatusDeviceStatus(scriptStatus string) (int8, bool) {
	switch strings.ToLower(strings.TrimSpace(scriptStatus)) {
	case "running":
		return 3, true
	case "paused":
		return 2, true
	case "idle":
		return 1, true
	default:
		return 0, false
	}
}

func heartbeatBusy(entry heartbeatEntry) bool {
	return entry.ScriptStatus == "running" || entry.ScriptStatus == "paused" ||
		entry.Status == 2 || entry.Status == 3
}

func mergeHeartbeat(current, incoming heartbeatEntry) heartbeatEntry {
	merged := current
	merged.DeviceID = incoming.DeviceID
	merged.UpdatedAt = incoming.UpdatedAt

	if incoming.IP != "" {
		merged.IP = incoming.IP
	}
	if incoming.Province != "" {
		merged.Province = incoming.Province
	}
	if incoming.City != "" {
		merged.City = incoming.City
	}
	if incoming.Battery > 0 {
		merged.Battery = incoming.Battery
	}
	if incoming.OutboxDepth >= 0 {
		merged.OutboxDepth = incoming.OutboxDepth
	}

	if incoming.Status == 0 {
		// 连接已断开。保留最后业务状态用于短时间重连恢复，但实时接口会
		// 根据在线连接强制显示离线。
		merged.Status = 0
		return merged
	}

	desiredStatus, hasScriptStatus := scriptStatusDeviceStatus(incoming.ScriptStatus)
	if !hasScriptStatus {
		if heartbeatBusy(merged) &&
			!merged.LastBusyAt.IsZero() &&
			incoming.UpdatedAt.Sub(merged.LastBusyAt) <= deviceBusyStateHold {
			if status, ok := scriptStatusDeviceStatus(merged.ScriptStatus); ok {
				merged.Status = status
			}
		} else if merged.Status == 0 {
			merged.Status = incoming.Status
		}
		return merged
	}

	incoming.ScriptStatus = strings.ToLower(strings.TrimSpace(incoming.ScriptStatus))
	if incoming.ScriptStatus == "running" || incoming.ScriptStatus == "paused" {
		merged.ScriptStatus = incoming.ScriptStatus
		merged.Status = desiredStatus
		merged.StateInstance = incoming.StateInstance
		merged.StateSeq = incoming.StateSeq
		merged.LastBusyAt = incoming.UpdatedAt
		return merged
	}

	sameInstance := incoming.StateInstance != "" &&
		incoming.StateInstance == merged.StateInstance
	sequenceValid := incoming.StateSeq == 0 || incoming.StateSeq >= merged.StateSeq
	explicitOwnerTransition := incoming.StateTransition && sameInstance && sequenceValid
	busyRecently := heartbeatBusy(merged) &&
		!merged.LastBusyAt.IsZero() &&
		incoming.UpdatedAt.Sub(merged.LastBusyAt) <= deviceBusyStateHold

	if busyRecently && !explicitOwnerTransition {
		// 另一个空闲代理实例不能覆盖仍在每10秒续租的运行实例。
		return merged
	}

	merged.ScriptStatus = "idle"
	merged.Status = desiredStatus
	merged.StateInstance = incoming.StateInstance
	merged.StateSeq = incoming.StateSeq
	return merged
}

func storeHeartbeat(entry heartbeatEntry) {
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = time.Now()
	}
	if entry.Status == 2 || entry.Status == 3 {
		entry.LastBusyAt = entry.UpdatedAt
	}

	heartbeatStateMu.Lock()
	if value, ok := heartbeatCache.Load(entry.DeviceID); ok {
		entry = mergeHeartbeat(value.(heartbeatEntry), entry)
	} else if entry.OutboxDepth < 0 {
		entry.OutboxDepth = 0
	}
	heartbeatCache.Store(entry.DeviceID, entry)
	heartbeatDirty.Store(entry.DeviceID, entry.UpdatedAt.UnixNano())
	heartbeatStateMu.Unlock()
}

func touchHeartbeat(deviceID string, status int8) {
	now := time.Now()
	entry := heartbeatEntry{
		DeviceID:  deviceID,
		Status:    status,
		UpdatedAt: now,
	}
	storeHeartbeat(entry)
}

type deviceLogEntry struct {
	LogType string
	Message string
}

func normalizeDeviceLogType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error":
		return "error"
	default:
		return "info"
	}
}

func truncateDeviceLog(message string) string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) > maxDeviceLogRunes {
		message = string(runes[:maxDeviceLogRunes])
	}
	return message
}

func normalizeDeviceLogs(raw interface{}) []deviceLogEntry {
	result := make([]deviceLogEntry, 0, 8)
	var appendLog func(interface{})
	appendLog = func(value interface{}) {
		if len(result) >= maxDeviceLogsBatch || value == nil {
			return
		}
		switch item := value.(type) {
		case []interface{}:
			for _, child := range item {
				appendLog(child)
				if len(result) >= maxDeviceLogsBatch {
					break
				}
			}
		case string:
			if message := truncateDeviceLog(item); message != "" {
				result = append(result, deviceLogEntry{LogType: "info", Message: message})
			}
		case map[string]interface{}:
			if entries, ok := item["entries"]; ok {
				appendLog(entries)
				return
			}
			message, _ := item["message"].(string)
			if message == "" {
				message, _ = item["msg"].(string)
			}
			logType, _ := item["log_type"].(string)
			if logType == "" {
				logType, _ = item["level"].(string)
			}
			if message = truncateDeviceLog(message); message != "" {
				result = append(result, deviceLogEntry{
					LogType: normalizeDeviceLogType(logType),
					Message: message,
				})
			}
		default:
			if encoded, err := json.Marshal(item); err == nil {
				appendLog(string(encoded))
			}
		}
	}
	appendLog(raw)
	return result
}

func enqueueDeviceLogs(deviceID string, raw interface{}) {
	entries := normalizeDeviceLogs(raw)
	if len(entries) == 0 || Hub == nil {
		return
	}
	if !Hub.enqueueDB(func(db *gorm.DB) {
		meta, ok := lookupDeviceMeta(db, deviceID)
		if !ok {
			// MQTT/WS 连接后，注册与第一批日志可能同时到达。不以
			// 设备记录已存在作为日志落库前提，先创建最小记录，后续 register 会补全设备信息。
			now := time.Now()
			placeholder := models.Device{
				DeviceID:      deviceID,
				Name:          deviceID,
				Status:        1,
				LastHeartbeat: &now,
				RegisterAt:    now,
			}
			if err := db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "device_id"}},
				DoNothing: true,
			}).Create(&placeholder).Error; err != nil {
				log.Printf("Create placeholder device for logs failed %s: %v", deviceID, err)
				return
			}
			meta, ok = lookupDeviceMeta(db, deviceID)
			if !ok {
				log.Printf("Device %s still unavailable for logs after placeholder create", deviceID)
				return
			}
		}
		records := make([]models.DeviceLog, 0, len(entries))
		for _, entry := range entries {
			records = append(records, models.DeviceLog{
				DeviceID: meta.ID,
				LogType:  entry.LogType,
				Message:  entry.Message,
			})
		}
		if err := db.CreateInBatches(&records, maxDeviceLogsBatch).Error; err != nil {
			log.Printf("Persist device logs failed for %s: %v", deviceID, err)
		}
	}) {
		log.Printf("DB queue full, dropping %d logs from %s", len(entries), deviceID)
	}
}

type heartbeatSnapshot struct {
	entry   heartbeatEntry
	version int64
}

// flushHeartbeats 使用大事务批量刷盘，显著减少SQLite锁和磁盘同步次数。
func flushHeartbeats(db *gorm.DB) {
	snapshots := make([]heartbeatSnapshot, 0, 256)
	heartbeatDirty.Range(func(key, value interface{}) bool {
		deviceID, ok := key.(string)
		if !ok {
			return true
		}
		version, ok := value.(int64)
		if !ok {
			return true
		}
		if cached, exists := heartbeatCache.Load(deviceID); exists {
			snapshots = append(snapshots, heartbeatSnapshot{
				entry:   cached.(heartbeatEntry),
				version: version,
			})
		}
		return true
	})
	if len(snapshots) == 0 {
		return
	}

	const batchSize = 250
	flushed := 0
	for start := 0; start < len(snapshots); start += batchSize {
		end := start + batchSize
		if end > len(snapshots) {
			end = len(snapshots)
		}
		batch := snapshots[start:end]
		records := make([]models.Device, 0, len(batch))
		for _, snapshot := range batch {
			entry := snapshot.entry
			heartbeatAt := entry.UpdatedAt
			records = append(records, models.Device{
				DeviceID:         entry.DeviceID,
				Name:             entry.DeviceID,
				Status:           entry.Status,
				IP:               entry.IP,
				Province:         entry.Province,
				City:             entry.City,
				AgentOutboxDepth: entry.OutboxDepth,
				LastHeartbeat:    &heartbeatAt,
				RegisterAt:       heartbeatAt,
			})
		}
		err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "ip", "province", "city", "agent_outbox_depth", "last_heartbeat",
			}),
		}).CreateInBatches(records, batchSize).Error
		if err != nil {
			log.Printf("Heartbeat flush failed: %v", err)
			continue
		}

		for _, snapshot := range batch {
			if current, ok := heartbeatDirty.Load(snapshot.entry.DeviceID); ok && current == snapshot.version {
				heartbeatDirty.Delete(snapshot.entry.DeviceID)
			}
		}
		flushed += len(batch)
	}
	if flushed > 0 {
		log.Printf("Heartbeat flushed: %d devices", flushed)
	}
}

// SendRawToDevice 向指定设备发送已编码消息。持有读锁直到入队完成，
// 避免与连接关闭并发时出现向已关闭channel发送的panic。
func (h *WSHub) SendRawToDevice(deviceID string, data []byte) error {
	h.mu.RLock()
	client, ok := h.clients[deviceID]
	externalMQTT := h.externalMQTT
	if !ok {
		h.mu.RUnlock()
		if externalMQTT != nil {
			return externalMQTT.PublishCommand(deviceID, data)
		}
		return ErrDeviceOffline
	}
	defer h.mu.RUnlock()
	select {
	case client.Send <- data:
		return nil
	default:
		return ErrSendQueueFull
	}
}

func (h *WSHub) SetExternalMQTT(bridge *ExternalMQTTBridge) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.externalMQTT = bridge
	h.mu.Unlock()
}

// SendToDevice 向指定设备发送消息。
func (h *WSHub) SendToDevice(deviceID string, message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return h.SendRawToDevice(deviceID, data)
}

type BatchSendResult struct {
	Sent      int `json:"sent"`
	Offline   int `json:"offline"`
	QueueFull int `json:"queue_full"`
}

// SendToDevices 对同一消息只编码一次，再向多台设备并发安全地入队。
func (h *WSHub) SendToDevices(deviceIDs []string, message interface{}) (BatchSendResult, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return BatchSendResult{}, err
	}
	h.mu.RLock()
	externalMQTT := h.externalMQTT
	h.mu.RUnlock()
	if externalMQTT != nil {
		result := BatchSendResult{}
		seen := make(map[string]struct{}, len(deviceIDs))
		for _, deviceID := range deviceIDs {
			if _, exists := seen[deviceID]; exists {
				continue
			}
			seen[deviceID] = struct{}{}
			switch err := h.SendRawToDevice(deviceID, data); {
			case err == nil:
				result.Sent++
			case errors.Is(err, ErrDeviceOffline):
				result.Offline++
			default:
				result.QueueFull++
			}
		}
		return result, nil
	}

	result := BatchSendResult{}
	h.mu.RLock()
	defer h.mu.RUnlock()
	seen := make(map[string]struct{}, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		if _, exists := seen[deviceID]; exists {
			continue
		}
		seen[deviceID] = struct{}{}
		client, ok := h.clients[deviceID]
		if !ok {
			result.Offline++
			continue
		}
		select {
		case client.Send <- data:
			result.Sent++
		default:
			result.QueueFull++
		}
	}
	return result, nil
}

// GetOnlineDevices 获取在线设备列表
func (h *WSHub) GetOnlineDevices() []string {
	h.mu.RLock()
	devices := make([]string, 0, len(h.clients))
	seen := make(map[string]struct{}, len(h.clients))
	for id := range h.clients {
		devices = append(devices, id)
		seen[id] = struct{}{}
	}
	h.mu.RUnlock()
	cutoff := time.Now().Add(-externalMQTTOnlineTTL)
	h.externalSeen.Range(func(key, value interface{}) bool {
		deviceID, idOK := key.(string)
		lastSeen, timeOK := value.(time.Time)
		if !idOK || !timeOK || lastSeen.Before(cutoff) {
			h.externalSeen.Delete(key)
			return true
		}
		if _, exists := seen[deviceID]; !exists {
			devices = append(devices, deviceID)
		}
		return true
	})
	return devices
}

func (h *WSHub) TouchExternalDevice(deviceID string) {
	if h != nil && deviceID != "" {
		h.externalSeen.Store(deviceID, time.Now())
	}
}

// IsMQTTDevice 判断设备当前是否由 MQTT 虚拟客户端占用主通道。
func (h *WSHub) IsMQTTDevice(deviceID string) bool {
	h.mu.RLock()
	client, ok := h.clients[deviceID]
	h.mu.RUnlock()
	if ok && client.Virtual {
		return true
	}
	lastSeen, ok := h.externalSeen.Load(deviceID)
	return ok && time.Since(lastSeen.(time.Time)) <= externalMQTTOnlineTTL
}

// WSMessage WebSocket消息结构
type WSMessage struct {
	Type    string      `json:"type"` // heartbeat, task, log, status, response
	Data    interface{} `json:"data"`
	Message string      `json:"message,omitempty"`
}

// DeviceWSHandler 设备WebSocket连接处理
func DeviceWSHandler(c *gin.Context) {
	deviceID := c.Param("device_id")
	if deviceID == "" {
		c.JSON(400, gin.H{"error": "device_id required"})
		return
	}
	if !validDeviceID(deviceID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device_id"})
		return
	}
	releaseSource, allowed := acquireWSConnection(c.ClientIP())
	if !allowed {
		c.Header("Retry-After", "10")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "device connection rate limited"})
		return
	}
	releaseOwned := true
	defer func() {
		if releaseOwned {
			releaseSource()
		}
	}()
	autoRegister := false
	autoRegisterRecovery := false
	userID := uint64(0)
	if config.App != nil && config.App.Security.DeviceAuthRequired {
		token := strings.TrimSpace(c.GetHeader("X-Device-Token"))
		if token == "" {
			token = strings.TrimSpace(c.Query("token"))
		}
		if Hub == nil || Hub.db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device authentication unavailable"})
			return
		}
		if device, ok := AuthenticateDevice(Hub.db, deviceID, token); !ok {
			if token == "" {
				if legacyDevice, legacyOK := AuthenticateLegacyTokenlessDevice(Hub.db, deviceID, c.ClientIP()); legacyOK {
					userID = legacyDevice.UserID
					log.Printf("Legacy tokenless device accepted from configured LAN: device=%s source_ip=%s", deviceID, c.ClientIP())
					goto deviceAuthenticationComplete
				}
			}
			var existing models.Device
			err := Hub.db.Where("device_id = ?", deviceID).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound) && canUseAutoRegistration(c):
				autoRegister = true
			case err == nil && canRecoverAutoRegistration(Hub.db, deviceID, c.ClientIP()) && canUseAutoRegistration(c):
				autoRegister = true
				autoRegisterRecovery = true
			default:
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid device credential"})
				return
			}
		} else {
			userID = device.UserID
		}
	}
deviceAuthenticationComplete:
	// MQTT 是设备主通道。迁移期间旧脚本可能继续重连 WS，
	// 这里拒绝同一 device_id 的备用连接，避免命令重复和状态覆盖。
	if Hub != nil && Hub.IsMQTTDevice(deviceID) {
		log.Printf("WebSocket fallback rejected while MQTT is active: %s", deviceID)
		c.Header("Connection", "close")
		c.JSON(http.StatusConflict, gin.H{"error": "mqtt transport active"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &WSClient{
		Conn:                 conn,
		DeviceID:             deviceID,
		UserID:               userID,
		AutoRegister:         autoRegister,
		AutoRegisterRecovery: autoRegisterRecovery,
		SourceIP:             c.ClientIP(),
		Send:                 make(chan []byte, wsSendQueueSize),
		ReleaseSource:        releaseSource,
	}

	if !Hub.enqueueRegister(client) {
		_ = conn.Close()
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device registration queue busy"})
		return
	}

	// 启动读写协程
	go client.writePump()
	go client.readPump()
	releaseOwned = false
}

func validDeviceID(deviceID string) bool {
	return len(deviceID) <= 128 && deviceIDPattern.MatchString(deviceID)
}

func allowAutoRegistration(key string) bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	autoRegisterLimiter.Lock()
	defer autoRegisterLimiter.Unlock()
	if now.Sub(autoRegisterLimiter.lastSweep) >= time.Minute {
		for source, timestamps := range autoRegisterLimiter.entries {
			kept := timestamps[:0]
			for _, at := range timestamps {
				if at.After(cutoff) {
					kept = append(kept, at)
				}
			}
			if len(kept) == 0 {
				delete(autoRegisterLimiter.entries, source)
			} else {
				autoRegisterLimiter.entries[source] = kept
			}
		}
		autoRegisterLimiter.lastSweep = now
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}
	limit := 10
	if config.App != nil && config.App.Security.DeviceAutoRegisterRateLimit > 0 {
		limit = config.App.Security.DeviceAutoRegisterRateLimit
	}
	if _, known := autoRegisterLimiter.entries[key]; !known && len(autoRegisterLimiter.entries) >= 4096 {
		return false
	}
	entries := autoRegisterLimiter.entries[key]
	kept := entries[:0]
	for _, at := range entries {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= limit {
		autoRegisterLimiter.entries[key] = kept
		return false
	}
	autoRegisterLimiter.entries[key] = append(kept, now)
	return true
}

func canUseAutoRegistration(c *gin.Context) bool {
	if config.App == nil || !config.App.Security.DeviceAutoRegister {
		return false
	}
	if config.App.Security.DeviceAutoRegisterRequireTLS && !requestUsesTLS(c.Request) {
		return false
	}
	return allowAutoRegistration(c.ClientIP())
}

// AuthenticateLegacyTokenlessDevice supports old agents that cannot store a
// per-device credential. It is disabled unless the operator configured a CIDR
// allowlist, and it only accepts an already-known device whose owner is active.
// Do not use it for public networks or broad/private CIDR ranges.
func AuthenticateLegacyTokenlessDevice(db *gorm.DB, deviceID, sourceIP string) (models.Device, bool) {
	var device models.Device
	if db == nil || config.App == nil || !legacyTokenlessSourceAllowed(sourceIP) {
		return device, false
	}
	if err := db.Where("device_id = ?", deviceID).First(&device).Error; err != nil || device.UserID == 0 {
		return models.Device{}, false
	}
	var owner models.User
	if err := db.Select("id", "status").First(&owner, device.UserID).Error; err != nil || owner.Status != 1 {
		return models.Device{}, false
	}
	return device, true
}

func legacyTokenlessSourceAllowed(sourceIP string) bool {
	ip := net.ParseIP(strings.TrimSpace(sourceIP))
	if ip == nil || config.App == nil {
		return false
	}
	for _, value := range config.App.Security.DeviceLegacyTokenlessCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// requestUsesTLS accepts a forwarded HTTPS marker only from the same local
// reverse proxy that main.go trusts for client-address forwarding. Accepting
// X-Forwarded-Proto from arbitrary direct clients would let plaintext device
// registration bypass the TLS-only bootstrap setting.
func requestUsesTLS(request *http.Request) bool {
	if request == nil {
		return false
	}
	if request.TLS != nil {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https") {
		return false
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return trustedProxyIP(ip)
}

func trustedProxyIP(ip net.IP) bool {
	if ip == nil || config.App == nil {
		return false
	}
	for _, value := range config.App.Security.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func canRecoverAutoRegistration(db *gorm.DB, deviceID, sourceIP string) bool {
	if db == nil || strings.TrimSpace(sourceIP) == "" {
		return false
	}
	var audit models.DeviceAutoRegistration
	return db.Where("device_id = ? AND source_ip = ? AND confirmed_at IS NULL AND recovery_expires_at > ?", deviceID, sourceIP, time.Now()).First(&audit).Error == nil
}

// writePump 向设备写消息
func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second) // 心跳间隔
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			// 发送心跳
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump 从设备读消息
func (c *WSClient) readPump() {
	defer func() {
		c.releaseSource()
		if Hub != nil {
			Hub.enqueueUnregister(c)
		}
		_ = c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxWSMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		touchHeartbeat(c.DeviceID, 1)
		return nil
	})

	invalidMessages := 0
	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		c.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			invalidMessages++
			if invalidMessages >= 3 {
				break
			}
			continue
		}
		invalidMessages = 0

		c.handleMessage(msg)
	}
}

type registrationInfo struct {
	Name            string
	Model           string
	OSVersion       string
	AgentVersion    string
	ProtocolVersion int
	Capabilities    string
	IP              string
	Province        string
	City            string
}

func persistRegistration(db *gorm.DB, deviceID string, info registrationInfo) {
	info = normalizeRegistrationInfo(info)
	var device models.Device
	result := db.Where("device_id = ?", deviceID).First(&device)
	now := time.Now()

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		device = models.Device{
			DeviceID:        deviceID,
			Name:            info.Name,
			Model:           info.Model,
			OSVersion:       info.OSVersion,
			AgentVersion:    info.AgentVersion,
			ProtocolVersion: info.ProtocolVersion,
			Capabilities:    info.Capabilities,
			IP:              info.IP,
			Province:        info.Province,
			City:            info.City,
			Status:          1,
			LastHeartbeat:   &now,
			RegisterAt:      now,
		}
		if device.Name == "" {
			device.Name = deviceID
		}
		if err := db.Create(&device).Error; err != nil {
			// 并发重连时另一worker可能已经创建，重新读取即可。
			if readErr := db.Where("device_id = ?", deviceID).First(&device).Error; readErr != nil {
				log.Printf("Failed to create device %s: %v", deviceID, err)
				return
			}
		}
	} else if result.Error != nil {
		log.Printf("Failed to query device %s: %v", deviceID, result.Error)
		return
	} else {
		updates := map[string]interface{}{
			"status":         1,
			"last_heartbeat": now,
		}
		if info.Name != "" {
			updates["name"] = info.Name
		}
		if info.Model != "" {
			updates["model"] = info.Model
		}
		if info.OSVersion != "" {
			updates["os_version"] = info.OSVersion
		}
		if info.AgentVersion != "" {
			updates["agent_version"] = info.AgentVersion
		}
		if info.ProtocolVersion > 0 {
			updates["protocol_version"] = info.ProtocolVersion
		}
		if info.Capabilities != "" {
			updates["capabilities"] = info.Capabilities
		}
		if info.IP != "" {
			updates["ip"] = info.IP
		}
		if info.Province != "" {
			updates["province"] = info.Province
		}
		if info.City != "" {
			updates["city"] = info.City
		}
		if err := db.Model(&device).Updates(updates).Error; err != nil {
			log.Printf("Failed to update device %s: %v", deviceID, err)
			return
		}
	}
	deviceMetaCache.Store(deviceID, deviceMeta{ID: device.ID, Name: device.Name})
}

func lookupDeviceMeta(db *gorm.DB, deviceID string) (deviceMeta, bool) {
	if cached, ok := deviceMetaCache.Load(deviceID); ok {
		return cached.(deviceMeta), true
	}
	var device models.Device
	if err := db.Select("id", "name").Where("device_id = ?", deviceID).First(&device).Error; err != nil {
		return deviceMeta{}, false
	}
	meta := deviceMeta{ID: device.ID, Name: device.Name}
	deviceMetaCache.Store(deviceID, meta)
	return meta, true
}

// handleMessage 处理设备消息
func (c *WSClient) handleMessage(msg WSMessage) {
	if c.AutoRegister && msg.Type != "register" {
		log.Printf("Auto-registration client %s sent %s before register", c.DeviceID, msg.Type)
		if c.Conn != nil {
			_ = c.Conn.Close()
		}
		return
	}
	switch msg.Type {
	case "register":
		if c.registrationHandled {
			return
		}
		info := registrationInfo{}
		if dataMap, ok := msg.Data.(map[string]interface{}); ok {
			info.Name, _ = dataMap["name"].(string)
			info.Model, _ = dataMap["model"].(string)
			info.OSVersion, _ = dataMap["os_version"].(string)
			info.AgentVersion = truncateUTF8(strings.TrimSpace(stringValue(dataMap["agent_version"])), 32)
			if value, ok := toFloat(dataMap["protocol_version"]); ok && value >= 1 && value <= 1000 {
				info.ProtocolVersion = int(value)
			}
			if capabilities, ok := dataMap["capabilities"].([]interface{}); ok {
				clean := make([]string, 0, len(capabilities))
				for _, capability := range capabilities {
					value := truncateUTF8(strings.TrimSpace(stringValue(capability)), 48)
					if value != "" && len(clean) < 32 {
						clean = append(clean, value)
					}
				}
				if encoded, err := json.Marshal(clean); err == nil {
					info.Capabilities = string(encoded)
				}
			}
			info.IP, _ = dataMap["ip"].(string)
			if loc, ok := dataMap["location"].(map[string]interface{}); ok {
				info.Province, _ = loc["province"].(string)
				info.City, _ = loc["city"].(string)
			}
		}
		deviceID := c.DeviceID
		if c.AutoRegister {
			if Hub == nil || Hub.db == nil {
				log.Printf("Auto-registration unavailable for device %s", deviceID)
				_ = c.Conn.Close()
				return
			}
			var token string
			var device models.Device
			var err error
			if c.AutoRegisterRecovery {
				token, device, err = RecoverAutoRegistrationDevice(Hub.db, deviceID, info, c.SourceIP)
			} else {
				token, device, err = AutoRegisterDevice(Hub.db, deviceID, info, c.SourceIP)
			}
			if err != nil {
				log.Printf("Auto-registration failed for device %s: %v", deviceID, err)
				_ = c.Conn.Close()
				return
			}
			c.UserID = device.UserID
			c.AutoRegister = false
			c.AutoRegisterRecovery = false
			c.registrationHandled = true
			deviceMetaCache.Store(deviceID, deviceMeta{ID: device.ID, Name: device.Name})
			resp := WSMessage{Type: "register", Message: "ok", Data: gin.H{
				"device_token": token, "server_protocol_version": 2,
				"heartbeat_idle_seconds": 30, "heartbeat_busy_seconds": 10,
			}}
			if data, err := json.Marshal(resp); err == nil {
				select {
				case c.Send <- data:
				default:
					log.Printf("Device %s send buffer full", c.DeviceID)
				}
			}
			return
		}
		c.registrationHandled = true
		if !Hub.enqueueDB(func(db *gorm.DB) {
			persistRegistration(db, deviceID, info)
		}) {
			log.Printf("Registration queue full for device %s", deviceID)
		}

		resp := WSMessage{Type: "register", Message: "ok", Data: gin.H{
			"server_protocol_version": 2, "heartbeat_idle_seconds": 30, "heartbeat_busy_seconds": 10,
		}}
		if data, err := json.Marshal(resp); err == nil {
			select {
			case c.Send <- data:
			default:
				log.Printf("Device %s send buffer full", c.DeviceID)
			}
		}

	case "register_ack":
		if Hub != nil && Hub.db != nil && c.UserID != 0 {
			if !Hub.enqueueDB(func(db *gorm.DB) {
				if err := ConfirmAutoRegistration(db, c.DeviceID, c.UserID); err != nil {
					log.Printf("Failed to confirm auto-registration for device %s: %v", c.DeviceID, err)
				}
			}) {
				log.Printf("Registration confirmation queue full for device %s", c.DeviceID)
			}
		}

	case "heartbeat":
		// 心跳数据写入内存缓存（不直接写DB，由 flushHeartbeats 批量刷盘）
		deviceStatus := int8(1)
		scriptStatus := ""
		ip := ""
		province := ""
		city := ""
		battery := 0
		outboxDepth := -1
		stateInstance := ""
		stateSeq := int64(0)
		stateTransition := false
		var heartbeatLogs interface{}
		if dataMap, ok := msg.Data.(map[string]interface{}); ok {
			if ss, ok := dataMap["script_status"].(string); ok {
				scriptStatus = ss
				switch ss {
				case "running":
					deviceStatus = 3
				case "paused":
					deviceStatus = 2
				}
			}
			if v, ok := dataMap["ip"].(string); ok {
				ip = v
			}
			if v, ok := dataMap["province"].(string); ok {
				province = v
			}
			if v, ok := dataMap["city"].(string); ok {
				city = v
			}
			if v, ok := toFloat(dataMap["battery"]); ok {
				battery = int(v)
			}
			if v, ok := toFloat(dataMap["outbox_depth"]); ok && v >= 0 && v <= 100000 {
				outboxDepth = int(v)
			}
			if v, ok := dataMap["instance_id"].(string); ok {
				stateInstance = strings.TrimSpace(v)
			}
			if v, ok := toFloat(dataMap["state_seq"]); ok {
				stateSeq = int64(v)
			}
			if v, ok := dataMap["state_transition"].(bool); ok {
				stateTransition = v
			}
			heartbeatLogs = dataMap["logs"]
		}
		storeHeartbeat(heartbeatEntry{
			DeviceID: c.DeviceID, Status: deviceStatus,
			IP: ip, Province: province, City: city,
			ScriptStatus: scriptStatus, Battery: battery, OutboxDepth: outboxDepth,
			StateInstance: stateInstance, StateSeq: stateSeq,
			StateTransition: stateTransition, UpdatedAt: time.Now(),
		})
		enqueueDeviceLogs(c.DeviceID, heartbeatLogs)

		// 响应心跳
		resp := WSMessage{Type: "heartbeat", Message: "ok"}
		if data, err := json.Marshal(resp); err == nil {
			select {
			case c.Send <- data:
			default:
			}
		}

	case "log", "log_batch":
		enqueueDeviceLogs(c.DeviceID, msg.Data)

	case "task_status":
		taskData, ok := msg.Data.(map[string]interface{})
		if !ok {
			break
		}
		tid, ok1 := toFloat(taskData["task_id"])
		st, ok2 := toFloat(taskData["status"])
		if !ok1 || !ok2 {
			log.Printf("Invalid task_status from %s: missing task_id or status", c.DeviceID)
			break
		}
		taskID := uint64(tid)
		status := int8(st)
		if status < 1 || status > 4 {
			log.Printf("Invalid task_status from %s: status=%d", c.DeviceID, status)
			break
		}
		result, _ := taskData["result"].(string)
		runID := truncateUTF8(strings.TrimSpace(stringValue(taskData["run_id"])), 96)
		eventID := truncateUTF8(strings.TrimSpace(stringValue(taskData["event_id"])), 128)
		if len(result) > 64*1024 {
			result = result[:64*1024]
		}
		deviceID := c.DeviceID
		if !Hub.enqueueDB(func(db *gorm.DB) {
			meta, exists := lookupDeviceMeta(db, deviceID)
			if !exists {
				log.Printf("Device %s not found for task_status", deviceID)
				return
			}
			updates := map[string]interface{}{
				"status": status,
				"result": result,
			}
			if status == 1 {
				now := time.Now()
				timeoutSeconds := defaultTaskTimeoutSeconds
				var task models.Task
				if err := db.Select("timeout_seconds").First(&task, taskID).Error; err == nil {
					timeoutSeconds = normalizeTaskTimeout(task.TimeoutSeconds)
				}
				updates["started_at"] = now
				updates["deadline_at"] = now.Add(time.Duration(timeoutSeconds) * time.Second)
			}
			if status >= 2 {
				updates["finished_at"] = time.Now()
				updates["deadline_at"] = nil
			}
			if err := db.Transaction(func(tx *gorm.DB) error {
				query := tx.Model(&models.TaskDevice{}).
					Where("task_id = ? AND device_id = ? AND status IN (0, 1)", taskID, meta.ID)
				if runID != "" {
					query = query.Where("run_id = ?", runID)
				}
				dbResult := query.Updates(updates)
				if dbResult.Error != nil {
					return dbResult.Error
				}
				if dbResult.RowsAffected == 0 {
					return nil
				}
				if status >= 2 {
					var pending int64
					tx.Model(&models.TaskDevice{}).Where("task_id = ? AND status IN (0, 1)", taskID).Count(&pending)
					if pending == 0 {
						_ = tx.Model(&models.Task{}).Where("id = ? AND status IN ?", taskID, []int{0, 1}).Update("status", 2).Error
					}
				} else if status == 1 {
					_ = tx.Model(&models.Task{}).Where("id = ? AND status = 0", taskID).Update("status", 1).Error
				}
				return tx.Create(&models.TaskLog{
					TaskID:   taskID,
					DeviceID: meta.ID,
					LogType:  "info",
					Message:  result,
				}).Error
			}); err != nil {
				log.Printf("Persist task status failed for %s: %v", deviceID, err)
				return
			}
			sendDeviceEventAck(deviceID, eventID)
		}) {
			log.Printf("DB queue full, dropping task status from %s", deviceID)
		}

	case "screenshot":
		if dataMap, ok := msg.Data.(map[string]interface{}); ok {
			imgB64, _ := dataMap["image"].(string)
			deviceID := c.DeviceID
			if imgB64 != "" && len(imgB64) <= maxScreenshotDataSize {
				if !Hub.enqueueMediaWeighted(int64(len(imgB64)), func() {
					// 去掉可能的 data:image/...;base64, 前缀
					if idx := strings.Index(imgB64, ","); idx != -1 {
						imgB64 = imgB64[idx+1:]
					}
					// 尝试解码（可能包含换行符）
					imgB64 = strings.ReplaceAll(imgB64, "\n", "")
					imgB64 = strings.ReplaceAll(imgB64, "\r", "")
					imgB64 = strings.ReplaceAll(imgB64, " ", "")
					if imgData, err := base64.StdEncoding.DecodeString(imgB64); err == nil {
						dir := screenshotDirectory()
						_ = os.MkdirAll(dir, 0755)
						if !reserveScreenshotBytes(dir, int64(len(imgData))) {
							log.Printf("Screenshot quota reached, dropping image from %s", deviceID)
							return
						}
						fn := fmt.Sprintf("%s_%d.jpg", sanitizeFilename(deviceID), time.Now().UnixNano())
						finalPath := filepath.Join(dir, fn)
						temporaryPath := finalPath + ".tmp"
						if err := os.WriteFile(temporaryPath, imgData, 0644); err == nil {
							if err := publishScreenshotFile(temporaryPath, finalPath, int64(len(imgData))); err != nil {
								_ = os.Remove(temporaryPath)
								log.Printf("Screenshot publish failed for %s: %v", deviceID, err)
								return
							}
							log.Printf("Screenshot saved: %s (%d bytes)", fn, len(imgData))
						} else {
							_ = os.Remove(temporaryPath)
							releaseScreenshotBytes(int64(len(imgData)))
						}
					} else {
						log.Printf("Screenshot base64 decode failed: %v", err)
					}
				}) {
					log.Printf("Media queue full, dropping screenshot from %s", deviceID)
				}
			} else if imgB64 != "" {
				log.Printf("Screenshot from %s exceeds %d bytes and was dropped", deviceID, maxScreenshotDataSize)
			}
		}
	case "screen_frame":
		if dataMap, ok := msg.Data.(map[string]interface{}); ok {
			if imgB64, ok := dataMap["image"].(string); ok &&
				imgB64 != "" && len(imgB64) <= maxFrameDataSize {
				accepted := false
				pressure := false
				FrameMutex.Lock()
				current, exists := FrameCache[c.DeviceID]
				projectedBytes := frameCacheBytes - len(current.Data) + len(imgB64)
				// 缓存满时仍允许已在推流的设备更新，避免所有画面冻结。
				if (exists || len(FrameCache) < maxFrameCacheSize) && projectedBytes <= maxFrameCacheBytes {
					frameCacheBytes = projectedBytes
					FrameCache[c.DeviceID] = frameEntry{Data: imgB64, UpdatedAt: time.Now()}
					accepted = true
				}
				pressure = projectedBytes >= maxFrameCacheBytes*8/10
				FrameMutex.Unlock()
				if !accepted || pressure {
					control, _ := json.Marshal(WSMessage{Type: "stream_control", Data: gin.H{
						"interval_ms": 2000, "quality": 10, "scale_percent": 30, "reason": "server_pressure",
					}})
					select {
					case c.Send <- control:
					default:
					}
				}
			}
		}
	case "sms_new":
		if dataMap, ok := msg.Data.(map[string]interface{}); ok {
			from := truncateUTF8(stringValue(dataMap["from"]), 64)
			body := truncateUTF8(stringValue(dataMap["body"]), 1024)
			smsTime := int64(0)
			if t, ok := toFloat(dataMap["date"]); ok {
				smsTime = int64(t)
			}
			deviceID := c.DeviceID
			if !Hub.enqueueDBWeighted(int64(len(from)+len(body)+256), func(db *gorm.DB) {
				meta, exists := lookupDeviceMeta(db, deviceID)
				if !exists {
					return
				}
				persistSMSBatch(db, meta.ID, []smsInput{{Sender: from, Body: body, SMSDate: smsTime}})
				Hub.enqueueMedia(func() {
					wxMsg := fmt.Sprintf("[%s] 短信\n发件人: %s\n内容: %s", meta.Name, from, body)
					payload, _ := json.Marshal(map[string]interface{}{
						"msgtype": "text",
						"text":    map[string]string{"content": wxMsg},
					})
					webhookURL := ""
					if config.App != nil {
						webhookURL = config.App.Security.NotificationWebhookURL
					}
					if webhookURL == "" {
						return
					}
					req, err := http.NewRequest(
						http.MethodPost,
						webhookURL,
						strings.NewReader(string(payload)),
					)
					if err != nil {
						return
					}
					req.Header.Set("Content-Type", "application/json")
					resp, err := notificationHTTPClient.Do(req)
					if err == nil {
						_ = resp.Body.Close()
					}
				})
			}) {
				log.Printf("DB queue full, dropping SMS from %s", deviceID)
			}
		}
	case "ack":
		if dataMap, ok := msg.Data.(map[string]interface{}); ok {
			cmdID, _ := dataMap["cmd_id"].(string)
			kind, _ := dataMap["kind"].(string)
			okValue, _ := dataMap["ok"].(bool)
			message, _ := dataMap["message"].(string)
			eventID := truncateUTF8(strings.TrimSpace(stringValue(dataMap["event_id"])), 128)
			log.Printf(
				"Device %s ACK cmd_id=%s kind=%s ok=%t message=%s",
				c.DeviceID, cmdID, kind, okValue, message,
			)
			if Hub != nil {
				Hub.AcknowledgeCommandDelivery(c.DeviceID, cmdID, okValue, message)
			}
			sendDeviceEventAck(c.DeviceID, eventID)
		}

	case "response":
		// 设备响应（read_sms/read_contacts等）
		if dataMap, ok := msg.Data.(map[string]interface{}); ok {
			cmd := truncateUTF8(stringValue(dataMap["command"]), 32)
			deviceID := c.DeviceID
			smsItems, contactItems, weight := compactDeviceResponse(cmd, dataMap)
			if cmd != "read_sms" && cmd != "read_contacts" {
				break
			}
			if !Hub.enqueueDBWeighted(weight, func(db *gorm.DB) {
				meta, exists := lookupDeviceMeta(db, deviceID)
				if !exists {
					return
				}
				if cmd == "read_sms" {
					persistSMSBatch(db, meta.ID, smsItems)
				}
				if cmd == "read_contacts" {
					persistContactBatch(db, meta.ID, contactItems)
				}
				log.Printf("Device %s response for %s processed", deviceID, cmd)
			}) {
				log.Printf("DB queue full, dropping response from %s", deviceID)
			}
		}
	}
}

func sendDeviceEventAck(deviceID, eventID string) {
	if Hub == nil || strings.TrimSpace(eventID) == "" {
		return
	}
	_ = Hub.SendToDevice(deviceID, WSMessage{Type: "event_ack", Data: map[string]interface{}{"event_id": eventID}})
}

type smsInput struct {
	Sender  string
	Body    string
	SMSDate int64
}

type contactInput struct {
	Name  string
	Phone string
}

func compactDeviceResponse(command string, data map[string]interface{}) ([]smsInput, []contactInput, int64) {
	weight := int64(1024)
	if command == "read_sms" {
		raw, _ := data["sms"].([]interface{})
		if len(raw) > maxResponseItems {
			raw = raw[:maxResponseItems]
		}
		items := make([]smsInput, 0, len(raw))
		for _, item := range raw {
			value, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			body := truncateUTF8(stringValue(value["body"]), 1024)
			if body == "" {
				continue
			}
			sender := truncateUTF8(stringValue(value["from"]), 64)
			date := int64(0)
			if parsed, ok := toFloat(value["date"]); ok {
				date = int64(parsed)
			}
			items = append(items, smsInput{Sender: sender, Body: body, SMSDate: date})
			weight += int64(len(sender) + len(body) + 64)
		}
		return items, nil, weight
	}
	if command == "read_contacts" {
		raw, _ := data["contacts"].([]interface{})
		if len(raw) > maxResponseItems {
			raw = raw[:maxResponseItems]
		}
		items := make([]contactInput, 0, len(raw))
		seenPhones := make(map[string]struct{}, len(raw))
		for _, item := range raw {
			value, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			phone := truncateUTF8(stringValue(value["phone"]), 32)
			if phone == "" {
				continue
			}
			if _, exists := seenPhones[phone]; exists {
				continue
			}
			seenPhones[phone] = struct{}{}
			name := truncateUTF8(stringValue(value["name"]), 128)
			items = append(items, contactInput{Name: name, Phone: phone})
			weight += int64(len(name) + len(phone) + 64)
		}
		return nil, items, weight
	}
	return nil, nil, weight
}

func persistSMSBatch(db *gorm.DB, deviceID uint64, items []smsInput) {
	if db == nil || len(items) == 0 {
		return
	}
	rows := make([]models.DeviceSms, 0, len(items))
	for _, item := range items {
		key := digestKey(strconv.FormatUint(deviceID, 10), item.Sender, item.Body, strconv.FormatInt(item.SMSDate, 10))
		rows = append(rows, models.DeviceSms{DeviceID: deviceID, Sender: item.Sender, Body: item.Body, Type: 1, SmsTime: item.SMSDate, DedupKey: &key})
	}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedup_key"}}, DoNothing: true}).CreateInBatches(&rows, 100).Error; err != nil {
		log.Printf("SMS batch persistence failed: %v", err)
	}
}

func persistContactBatch(db *gorm.DB, deviceID uint64, items []contactInput) {
	if db == nil {
		return
	}
	rows := make([]models.DeviceContact, 0, len(items))
	for _, item := range items {
		key := digestKey(strconv.FormatUint(deviceID, 10), item.Phone)
		rows = append(rows, models.DeviceContact{DeviceID: deviceID, Name: item.Name, Phone: item.Phone, DedupKey: &key})
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("device_id = ?", deviceID).Delete(&models.DeviceContact{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedup_key"}}, DoNothing: true}).CreateInBatches(&rows, 100).Error
	})
	if err != nil {
		log.Printf("Contact batch persistence failed: %v", err)
	}
}

func digestKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", sum)
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func truncateUTF8(value string, maxRunes int) string {
	if maxRunes < 1 {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}

// ============================================
// HTTP API: 通过WebSocket下发任务
// ============================================

// GetDeviceSms 获取设备短信
func (h *WebSocketHandler) GetDeviceSms(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceStringAccess(c, h.DB, id) {
		return
	}
	var dev models.Device
	if h.DB.Where("device_id = ?", id).First(&dev).Error != nil {
		c.JSON(200, models.Response{Code: 200, Message: "成功", Data: []interface{}{}})
		return
	}
	var list []models.DeviceSms
	h.DB.Where("device_id = ?", dev.ID).Order("sms_time DESC, id DESC").Limit(200).Find(&list)
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: list})
}

// GetDeviceContacts 获取设备通讯录
func (h *WebSocketHandler) GetDeviceContacts(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceStringAccess(c, h.DB, id) {
		return
	}
	var dev models.Device
	if h.DB.Where("device_id = ?", id).First(&dev).Error != nil {
		c.JSON(200, models.Response{Code: 200, Message: "成功", Data: []interface{}{}})
		return
	}
	var list []models.DeviceContact
	h.DB.Where("device_id = ?", dev.ID).Order("name ASC").Limit(500).Find(&list)
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: list})
}

// DeleteDeviceSms 删除设备短信（单条或全部）
func (h *WebSocketHandler) DeleteDeviceSms(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceStringAccess(c, h.DB, id) {
		return
	}
	smsID := c.Param("smsId")
	var dev models.Device
	if h.DB.Where("device_id = ?", id).First(&dev).Error != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return
	}
	if smsID != "" {
		h.DB.Where("id = ? AND device_id = ?", smsID, dev.ID).Delete(&models.DeviceSms{})
	} else {
		h.DB.Where("device_id = ?", dev.ID).Delete(&models.DeviceSms{})
	}
	c.JSON(200, models.Response{Code: 200, Message: "已删除"})
}

// DeleteDeviceContacts 删除设备通讯录（单条或全部）
func (h *WebSocketHandler) DeleteDeviceContacts(c *gin.Context) {
	id := c.Param("id")
	if !ensureDeviceStringAccess(c, h.DB, id) {
		return
	}
	contactID := c.Param("contactId")
	var dev models.Device
	if h.DB.Where("device_id = ?", id).First(&dev).Error != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return
	}
	if contactID != "" {
		h.DB.Where("id = ? AND device_id = ?", contactID, dev.ID).Delete(&models.DeviceContact{})
	} else {
		h.DB.Where("device_id = ?", dev.ID).Delete(&models.DeviceContact{})
	}
	c.JSON(200, models.Response{Code: 200, Message: "已删除"})
}

type WebSocketHandler struct {
	DB *gorm.DB
}

var (
	douyinRoomIDPattern      = regexp.MustCompile(`\\?"roomId\\?"\s*:\s*\\?"([0-9]+)\\?"`)
	douyinRoomIDSnakePattern = regexp.MustCompile(`\\?"room_id\\?"\s*:\s*\\?"([0-9]+)\\?"`)
	douyinRoomIDQueryPattern = regexp.MustCompile(`(?i)(?:[?&]room_id=|[?&]roomId=)([0-9]+)`)
	douyinURLPattern         = regexp.MustCompile(`https?://\S+`)
)

// resolveDouyinLiveRoomID 将纯 ID、room_id 参数或抖音分享链接统一解析为真实 room_id。
// live.douyin.com/{数字} 中的数字是 web_rid，不一定是真正的 room_id，
// 因此分享链接需要读取页面内嵌的 roomId 字段。
func resolveDouyinLiveRoomID(input string) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", errors.New("请填写直播间ID或分享链接")
	}
	if match := douyinRoomIDQueryPattern.FindStringSubmatch(raw); len(match) == 2 {
		return match[1], nil
	}

	var link string
	if isDigitsOnly(raw) {
		// 当前真实 room_id 通常是 18~19 位；较短的直播页数字是 web_rid，
		// 需要访问直播页提取真正的 roomId。
		if len(raw) >= 17 {
			return raw, nil
		}
		link = "https://live.douyin.com/" + raw
	} else {
		link = douyinURLPattern.FindString(raw)
	}
	if link == "" {
		return "", errors.New("直播间ID或分享链接格式不正确")
	}
	link = strings.TrimRight(link, ")]}，。")
	parsed, err := url.Parse(link)
	if err != nil {
		return "", errors.New("分享链接格式不正确")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "douyin.com" && host != "www.douyin.com" &&
		host != "live.douyin.com" && host != "v.douyin.com" {
		return "", errors.New("只支持抖音直播分享链接")
	}

	client := &http.Client{Timeout: 12 * time.Second}
	request, err := http.NewRequest(http.MethodGet, link, nil)
	if err != nil {
		return "", fmt.Errorf("创建分享链接请求失败: %v", err)
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	resp, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("解析分享链接失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", fmt.Errorf("读取分享页面失败: %v", err)
	}
	for _, pattern := range []*regexp.Regexp{douyinRoomIDPattern, douyinRoomIDSnakePattern} {
		if match := pattern.FindSubmatch(body); len(match) == 2 {
			return string(match[1]), nil
		}
	}

	// 短链可能经过重定向，重定向后的 URL 仍可能携带 room_id。
	if resp.Request != nil {
		if match := douyinRoomIDQueryPattern.FindStringSubmatch(resp.Request.URL.RawQuery); len(match) == 2 {
			return match[1], nil
		}
	}
	return "", errors.New("分享链接中没有找到真实room_id，可能直播已结束或链接已失效")
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// directLiveRoomTaskScript 是批量控制中的临时任务脚本。
// 它由现有 main.js 的通用任务执行器运行，不需要修改手机端业务脚本。
const directLiveRoomTaskScript = `
var raw = params && params.live_room_id;
if (raw === undefined || raw === null || String(raw).trim() === "") {
	throw new Error("请填写直播间ID或分享链接");
}
raw = String(raw).trim();
var id = raw;
var queryMatch = raw.match(/(?:[?&]|^)room_id=([0-9]+)/i);
if (!queryMatch) {
	queryMatch = raw.match(/(?:[?&]|^)roomId=([0-9]+)/i);
}
if (queryMatch) {
	id = queryMatch[1];
} else {
	var pathMatch = raw.match(/(?:live\.douyin\.com\/|\/live\/|\/room\/)([0-9]+)/i);
	if (pathMatch) {
		id = pathMatch[1];
	}
}
var uri = "";
if (/^[0-9]+$/.test(id)) {
	uri = "snssdk1128://live?room_id=" + id;
} else {
	var urlMatch = raw.match(/https?:\/\/\S+/i);
	if (urlMatch) {
		uri = urlMatch[0].replace(/[)\]}，。]+$/g, "");
	} else {
		throw new Error("直播间ID或分享链接格式不正确");
	}
}
if (typeof report === "function") {
	report("批量控制：进入直播间，跳转地址=" + uri);
}
utils.openActivity({ uri: uri });
sleep(3500);
return "已发起直播间跳转";
`

// PushTaskToDevice 向设备推送任务
func (h *WebSocketHandler) PushTaskToDevice(c *gin.Context) {
	var req struct {
		DeviceID string `json:"device_id" binding:"required"`
		TaskID   uint64 `json:"task_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if !ensureTaskAccess(c, h.DB, strconv.FormatUint(req.TaskID, 10), true) ||
		!ensureDeviceStringAccess(c, h.DB, req.DeviceID) {
		return
	}

	// 获取任务信息
	var task models.Task
	if err := h.DB.Preload("Script").First(&task, req.TaskID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "任务不存在"})
		return
	}

	// 获取设备信息
	var device models.Device
	if err := h.DB.Where("device_id = ?", req.DeviceID).First(&device).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return
	}

	// 合并参数
	var scriptContent string
	mergedParams := MergeParams("", task.Params, device.DeviceParams)
	if task.Script != nil {
		mergedParams = MergeParams(task.Script.Params, task.Params, device.DeviceParams)
		scriptContent = task.Script.Content
	}

	// 创建消息
	msg := WSMessage{
		Type: "task",
		Data: gin.H{
			"cmd_id":    newCommandID("task"),
			"task_id":   task.ID,
			"task_name": task.Name,
			"script":    scriptContent,
			"params":    mergedParams,
		},
	}

	if err := Hub.SendCommandWithDelivery(req.DeviceID, msg, req.TaskID); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "推送失败"})
		return
	}

	// 更新任务设备状态
	h.DB.Model(&models.TaskDevice{}).
		Where("task_id = ? AND device_id = ?", req.TaskID, device.ID).
		Updates(map[string]interface{}{
			"status":     1,
			"started_at": time.Now(),
		})

	// 记录日志
	h.DB.Create(&models.TaskLog{
		TaskID:   req.TaskID,
		DeviceID: device.ID,
		LogType:  "info",
		Message:  "任务已下发到设备",
	})

	c.JSON(200, models.Response{Code: 200, Message: "任务已推送"})
}

// PushCommandToDevice 向设备推送命令
func (h *WebSocketHandler) PushCommandToDevice(c *gin.Context) {
	var req struct {
		DeviceID string                 `json:"device_id" binding:"required"`
		Command  string                 `json:"command" binding:"required"`
		Params   map[string]interface{} `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if !ensureDeviceStringAccess(c, h.DB, req.DeviceID) {
		return
	}

	msg := WSMessage{
		Type: "command",
		Data: gin.H{
			"cmd_id":  newCommandID("cmd"),
			"command": req.Command,
			"params":  req.Params,
		},
	}

	if err := Hub.SendCommandWithDelivery(req.DeviceID, msg, 0); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "推送失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "命令已推送"})
}

// PushCommandToDevices 批量向设备推送同一命令，避免前端逐台发HTTP请求。
func (h *WebSocketHandler) PushCommandToDevices(c *gin.Context) {
	var req struct {
		DeviceIDs []string               `json:"device_ids" binding:"required"`
		Command   string                 `json:"command" binding:"required"`
		Params    map[string]interface{} `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.DeviceIDs) == 0 {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if len(req.DeviceIDs) > 10000 {
		c.JSON(200, models.Response{Code: 400, Message: "单次最多下发10000台设备"})
		return
	}
	for _, deviceID := range req.DeviceIDs {
		if !ensureDeviceStringAccess(c, h.DB, strings.TrimSpace(deviceID)) {
			return
		}
	}

	unique := make([]string, 0, len(req.DeviceIDs))
	seen := make(map[string]struct{}, len(req.DeviceIDs))
	for _, id := range req.DeviceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	msg := WSMessage{
		Type: "command",
		Data: gin.H{
			"cmd_id":  newCommandID("cmd"),
			"command": req.Command,
			"params":  req.Params,
		},
	}
	if req.Command == "enter_live_room" {
		rawRoomID, _ := req.Params["live_room_id"].(string)
		roomID, resolveErr := resolveDouyinLiveRoomID(rawRoomID)
		if resolveErr != nil {
			c.JSON(200, models.Response{Code: 400, Message: resolveErr.Error()})
			return
		}
		// 使用通用 task 执行器直接调用 utils.openActivity，保持手机端脚本零修改。
		msg = WSMessage{
			Type: "task",
			Data: gin.H{
				"cmd_id":    newCommandID("live-room"),
				"task_name": "批量进入直播间",
				"script":    directLiveRoomTaskScript,
				"params":    map[string]interface{}{"live_room_id": roomID},
			},
		}
	}
	result, err := Hub.SendCommandsWithDelivery(unique, msg, 0)
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "批量推送失败"})
		return
	}
	c.JSON(200, models.Response{
		Code:    200,
		Message: fmt.Sprintf("已发送%d台，离线%d台，队列繁忙%d台", result.Sent, result.Offline, result.QueueFull),
		Data:    result,
	})
}

// GetOnlineDevices 获取在线设备列表
func (h *WebSocketHandler) GetOnlineDevices(c *gin.Context) {
	devices := Hub.GetOnlineDevices()
	if !middleware.IsSystemAdminUser(h.DB, middleware.GetUserID(c)) {
		var allowed []string
		h.DB.Model(&models.Device{}).Where("user_id = ? AND device_id IN ?", middleware.GetUserID(c), devices).Pluck("device_id", &allowed)
		devices = allowed
	}
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: devices})
}

// DeviceRealtimeInfo 设备实时信息（DB基础信息 + 心跳内存实时数据）
type DeviceRealtimeInfo struct {
	ID            uint64              `json:"id"`
	DeviceID      string              `json:"device_id"`
	Name          string              `json:"name"`
	Model         string              `json:"model"`
	OSVersion     string              `json:"os_version"`
	IP            string              `json:"ip"`
	Status        int8                `json:"status"`        // 实时：心跳缓存
	ScriptStatus  string              `json:"script_status"` // 实时：心跳缓存
	Battery       int                 `json:"battery"`       // 实时：心跳缓存
	Province      string              `json:"province"`
	City          string              `json:"city"`
	GroupID       uint64              `json:"group_id"`
	Group         *models.DeviceGroup `json:"group,omitempty"`
	Online        bool                `json:"online"` // 实时：WebSocket是否连接
	LastHeartbeat *time.Time          `json:"last_heartbeat"`
}

// GetDevicesRealtime 获取设备列表（含实时心跳状态）
func (h *WebSocketHandler) GetDevicesRealtime(c *gin.Context) {
	paginated := c.Query("page") != "" || c.Query("size") != "" ||
		c.Query("keyword") != "" || c.Query("status") != "" || c.Query("group_id") != ""
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 500 {
		size = 500
	}

	query := h.DB.Model(&models.Device{})
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR device_id LIKE ? OR model LIKE ? OR ip LIKE ?", like, like, like, like)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if groupID := c.Query("group_id"); groupID != "" {
		query = query.Where("group_id = ?", groupID)
	}
	if userID := middleware.GetUserID(c); !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ?", userID)
	}

	var total int64
	if paginated {
		query.Count(&total)
	}
	var devices []models.Device
	findQuery := query.Preload("Group").Order("id DESC")
	if paginated {
		findQuery = findQuery.Offset((page - 1) * size).Limit(size)
	}
	findQuery.Find(&devices)

	onlineDevices := Hub.GetOnlineDevices()
	onlineSet := make(map[string]bool, len(onlineDevices))
	for _, id := range onlineDevices {
		onlineSet[id] = true
	}

	result := make([]DeviceRealtimeInfo, 0, len(devices))
	for _, dev := range devices {
		info := DeviceRealtimeInfo{
			ID:            dev.ID,
			DeviceID:      dev.DeviceID,
			Name:          dev.Name,
			Model:         dev.Model,
			OSVersion:     dev.OSVersion,
			IP:            dev.IP,
			Status:        dev.Status,
			Province:      dev.Province,
			City:          dev.City,
			GroupID:       dev.GroupID,
			Group:         dev.Group,
			Online:        onlineSet[dev.DeviceID],
			LastHeartbeat: dev.LastHeartbeat,
		}
		// 用实时心跳数据覆盖
		if val, ok := heartbeatCache.Load(dev.DeviceID); ok {
			hb := val.(heartbeatEntry)
			info.Status = hb.Status
			info.ScriptStatus = hb.ScriptStatus
			info.Battery = hb.Battery
			if hb.IP != "" {
				info.IP = hb.IP
			}
			if hb.Province != "" {
				info.Province = hb.Province
			}
			if hb.City != "" {
				info.City = hb.City
			}
		}
		if onlineSet[dev.DeviceID] {
			// 防御性归一化：页面状态始终以脚本实时状态为准，避免重连注册
			// 把 running 临时降成普通在线。
			if status, ok := scriptStatusDeviceStatus(info.ScriptStatus); ok {
				info.Status = status
			}
		}
		if !onlineSet[dev.DeviceID] {
			info.Status = 0
			info.ScriptStatus = ""
		}
		result = append(result, info)
	}

	if paginated {
		c.JSON(200, models.PageResponse{
			Code: 200, Message: "成功", Data: result,
			Total: total, Page: page, Size: size,
		})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: result})
}

// ListScreenshots 获取截图列表
func (h *WebSocketHandler) ListScreenshots(c *gin.Context) {
	deviceID := c.Query("device_id")
	if deviceID != "" && !ensureDeviceStringAccess(c, h.DB, deviceID) {
		return
	}
	dir := screenshotDirectory()
	os.MkdirAll(dir, 0755)

	entries, err := os.ReadDir(dir)
	if err != nil {
		c.JSON(200, models.Response{Code: 200, Message: "成功", Data: []interface{}{}})
		return
	}

	type ScreenshotInfo struct {
		Filename string `json:"filename"`
		DeviceID string `json:"device_id"`
		Time     int64  `json:"time"`
		URL      string `json:"url"`
		Size     int64  `json:"size"`
	}

	allowedDevices := map[string]bool{}
	isAdmin := middleware.IsSystemAdminUser(h.DB, middleware.GetUserID(c))
	if !isAdmin {
		var ids []string
		h.DB.Model(&models.Device{}).Where("user_id = ?", middleware.GetUserID(c)).Pluck("device_id", &ids)
		for _, id := range ids {
			allowedDevices[sanitizeFilename(id)] = true
		}
	}
	list := make([]ScreenshotInfo, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jpg") {
			continue
		}
		info, _ := e.Info()
		// 格式: DEVICEID_TIMESTAMP.jpg
		name := strings.TrimSuffix(e.Name(), ".jpg")
		parts := strings.SplitN(name, "_", 2)
		if len(parts) == 2 {
			if deviceID != "" && parts[0] != sanitizeFilename(deviceID) {
				continue
			}
			if deviceID == "" && !allowedDevices[parts[0]] {
				continue
			}
			var ts int64
			fmt.Sscanf(parts[1], "%d", &ts)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			list = append(list, ScreenshotInfo{
				Filename: e.Name(),
				DeviceID: parts[0],
				Time:     ts,
				URL:      "/api/v1/ws/screenshot-file/" + e.Name() + "?device_id=" + url.QueryEscape(parts[0]),
				Size:     size,
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Time > list[j].Time
	})
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "100"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 100
	}
	if size > 500 {
		size = 500
	}
	total := int64(len(list))
	start := (page - 1) * size
	if start > len(list) {
		start = len(list)
	}
	end := start + size
	if end > len(list) {
		end = len(list)
	}
	c.JSON(200, models.PageResponse{
		Code: 200, Message: "成功", Data: list[start:end],
		Total: total, Page: page, Size: size,
	})
}

// GetScreenFrames 获取实时屏幕帧
func (h *WebSocketHandler) GetScreenFrames(c *gin.Context) {
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	wanted := make(map[string]struct{})
	requested := strings.TrimSpace(c.Query("device_ids"))
	if raw := requested; raw != "" {
		for _, id := range strings.Split(raw, ",") {
			id = strings.TrimSpace(id)
			if id != "" && len(wanted) < maxScreenWallDevices {
				wanted[id] = struct{}{}
			}
		}
	}
	if !middleware.IsSystemAdminUser(h.DB, middleware.GetUserID(c)) {
		var allowed []string
		h.DB.Model(&models.Device{}).Where("user_id = ?", middleware.GetUserID(c)).Pluck("device_id", &allowed)
		allowedSet := make(map[string]struct{}, len(allowed))
		for _, id := range allowed {
			allowedSet[id] = struct{}{}
		}
		for id := range wanted {
			if _, ok := allowedSet[id]; !ok {
				delete(wanted, id)
			}
		}
		if len(wanted) == 0 && requested == "" {
			for id := range allowedSet {
				if len(wanted) >= maxScreenWallDevices {
					break
				}
				wanted[id] = struct{}{}
			}
		}
		if requested != "" && len(wanted) == 0 {
			c.JSON(200, models.Response{Code: 200, Message: "成功", Data: map[string]string{}})
			return
		}
	}
	FrameMutex.Lock()
	defer FrameMutex.Unlock()
	now := time.Now()
	sinceMillis, _ := strconv.ParseInt(strings.TrimSpace(c.Query("since")), 10, 64)
	frames := make(map[string]string)
	responseBytes := 0
	latestMillis := sinceMillis
	for k, v := range FrameCache {
		// 超过10秒的帧视为过期，清理掉
		if now.Sub(v.UpdatedAt) > 10*time.Second {
			frameCacheBytes -= len(v.Data)
			delete(FrameCache, k)
			continue
		}
		if len(wanted) > 0 {
			if _, ok := wanted[k]; !ok {
				continue
			}
		} else if len(frames) >= maxScreenWallDevices {
			continue
		}
		updatedMillis := v.UpdatedAt.UnixMilli()
		if sinceMillis > 0 && updatedMillis <= sinceMillis {
			continue
		}
		if responseBytes+len(v.Data) > maxScreenResponseSize {
			continue
		}
		frames[k] = "data:image/jpeg;base64," + v.Data
		responseBytes += len(v.Data)
		if updatedMillis > latestMillis {
			latestMillis = updatedMillis
		}
	}
	c.Header("X-Screen-Frame-Version", strconv.FormatInt(latestMillis, 10))
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: frames})
}

// toFloat 将 JSON 数字转为 float64
func toFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	}
	return 0, false
}

// GetDeviceMetrics 获取设备指标
func (h *WebSocketHandler) GetDeviceMetrics(c *gin.Context) {
	deviceID := c.Param("id")
	if !ensureDeviceStringAccess(c, h.DB, deviceID) {
		return
	}
	hours := 24
	if h := c.Query("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v > 0 {
			hours = v
		}
	}
	var dev models.Device
	if err := h.DB.Where("device_id = ?", deviceID).First(&dev).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return
	}
	var metrics []models.DeviceMetric
	h.DB.Where("device_id = ? AND created_at > ?", dev.ID, time.Now().Add(-time.Duration(hours)*time.Hour)).
		Order("created_at ASC").Limit(500).Find(&metrics)
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: metrics})
}

func (h *WebSocketHandler) DownloadScreenshot(c *gin.Context) {
	userID, ok := authenticateScreenshotRequest(c, h.DB)
	if !ok {
		c.JSON(http.StatusUnauthorized, models.Response{Code: 401, Message: "截图访问凭证无效"})
		return
	}
	filename := c.Param("filename")
	filename = filepath.Base(strings.TrimPrefix(filename, "/"))
	if filename == "." || filename == ".." || strings.Contains(filename, "..") {
		c.JSON(400, models.Response{Code: 400, Message: "非法文件名"})
		return
	}
	deviceID := strings.TrimSpace(c.Query("device_id"))
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, models.Response{Code: 400, Message: "缺少设备ID"})
		return
	}
	var device models.Device
	if err := h.DB.Where("device_id = ?", deviceID).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, models.Response{Code: 404, Message: "设备不存在"})
		return
	}
	if !middleware.IsSystemAdminUser(h.DB, userID) && device.UserID != userID {
		c.JSON(http.StatusForbidden, models.Response{Code: 403, Message: "无权访问此截图"})
		return
	}
	if !strings.HasPrefix(filename, sanitizeFilename(deviceID)+"_") {
		c.JSON(http.StatusForbidden, models.Response{Code: 403, Message: "截图设备不匹配"})
		return
	}
	filePath := filepath.Join(screenshotDirectory(), filename)
	data, err := os.ReadFile(filePath)
	if err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "文件不存在"})
		return
	}
	c.Data(200, "image/jpeg", data)
}

func authenticateScreenshotRequest(c *gin.Context, db *gorm.DB) (uint64, bool) {
	token := strings.TrimSpace(c.Query("access_token"))
	if token == "" {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token = strings.TrimSpace(parts[1])
		}
	}
	claims, err := middleware.ParseToken(token)
	if err != nil || claims.UserID == 0 {
		return 0, false
	}
	var user models.User
	if db == nil || db.Select("id", "status").First(&user, claims.UserID).Error != nil || user.Status != 1 {
		return 0, false
	}
	return claims.UserID, true
}

func NewWebSocketHandler(db *gorm.DB) *WebSocketHandler {
	return &WebSocketHandler{DB: db}
}
