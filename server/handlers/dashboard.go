package handlers

import (
	"fmt"
	"sync"
	"time"

	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DashboardHandler struct {
	DB *gorm.DB
}

type dashboardCacheEntry struct {
	stats     models.DashboardStats
	expiresAt time.Time
}

var dashboardStatsCache = struct {
	sync.Mutex
	entries map[string]dashboardCacheEntry
}{entries: make(map[string]dashboardCacheEntry)}

// GetStats 获取仪表盘统计数据
func (h *DashboardHandler) GetStats(c *gin.Context) {
	var stats models.DashboardStats
	userID := middleware.GetUserID(c)
	admin := middleware.IsSystemAdminUser(h.DB, userID)
	cacheKey := fmt.Sprintf("%t:%d", admin, userID)
	now := time.Now()
	dashboardStatsCache.Lock()
	if cached, ok := dashboardStatsCache.entries[cacheKey]; ok && now.Before(cached.expiresAt) {
		dashboardStatsCache.Unlock()
		c.JSON(200, models.Response{Code: 200, Message: "成功", Data: cached.stats})
		return
	}
	dashboardStatsCache.Unlock()

	deviceQuery := h.DB.Model(&models.Device{})
	taskQuery := h.DB.Model(&models.Task{})
	scriptQuery := h.DB.Model(&models.Script{})
	resourceQuery := h.DB.Model(&models.Resource{})
	if !admin {
		deviceQuery = deviceQuery.Where("user_id = ?", userID)
		taskQuery = taskQuery.Where("user_id = ?", userID)
		scriptQuery = scriptQuery.Where("user_id = ?", userID)
		resourceQuery = resourceQuery.Where("user_id = ?", userID)
	}
	var deviceCounts struct{ Total, Online int64 }
	deviceQuery.Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN status > 0 THEN 1 ELSE 0 END), 0) AS online").Scan(&deviceCounts)
	stats.DeviceCount, stats.OnlineCount = deviceCounts.Total, deviceCounts.Online
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var taskCounts struct{ Total, Running, Today int64 }
	taskQuery.Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN status = 1 THEN 1 ELSE 0 END), 0) AS running, COALESCE(SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END), 0) AS today", dayStart, dayStart.Add(24*time.Hour)).Scan(&taskCounts)
	stats.TaskCount, stats.RunningCount, stats.TodayTaskCount = taskCounts.Total, taskCounts.Running, taskCounts.Today
	scriptQuery.Count(&stats.ScriptCount)
	resourceQuery.Count(&stats.ResourceCount)
	if admin {
		h.DB.Model(&models.User{}).Count(&stats.UserCount)
	}
	dashboardStatsCache.Lock()
	dashboardStatsCache.entries[cacheKey] = dashboardCacheEntry{stats: stats, expiresAt: now.Add(5 * time.Second)}
	if len(dashboardStatsCache.entries) > 1024 {
		for key, entry := range dashboardStatsCache.entries {
			if now.After(entry.expiresAt) {
				delete(dashboardStatsCache.entries, key)
			}
		}
	}
	dashboardStatsCache.Unlock()

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: stats})
}

// GetRealtime 获取实时数据
func (h *DashboardHandler) GetRealtime(c *gin.Context) {
	var onlineDevices []models.Device
	deviceQuery := h.DB.Where("status > 0")
	taskQuery := h.DB.Order("created_at DESC")
	userID := middleware.GetUserID(c)
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		deviceQuery = deviceQuery.Where("user_id = ?", userID)
		taskQuery = taskQuery.Where("user_id = ?", userID)
	}
	deviceQuery.Limit(10).Find(&onlineDevices)

	var recentTasks []models.Task
	taskQuery.Limit(5).Find(&recentTasks)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: gin.H{
		"online_devices": onlineDevices,
		"recent_tasks":   recentTasks,
	}})
}

func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{DB: db}
}
