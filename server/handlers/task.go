package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskHandler struct {
	DB *gorm.DB
}

type taskDispatchTarget struct {
	TaskDeviceID uint64 `gorm:"column:task_device_id"`
	TaskID       uint64 `gorm:"column:task_id"`
	DeviceID     string `gorm:"column:device_id"`
	DeviceParams string `gorm:"column:device_params"`
	RunID        string `gorm:"column:run_id"`
}

type taskDispatchResult struct {
	Sent    int `json:"sent"`
	Offline int `json:"offline"`
	Busy    int `json:"busy"`
	Queued  int `json:"queued"`
}

type taskCreateRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	ScriptID    uint64   `json:"script_id" binding:"required"`
	Params      string   `json:"params"`
	CronExpr    string   `json:"cron_expr"`
	CronEnabled bool     `json:"cron_enabled"`
	DeviceIDs   []uint64 `json:"device_ids"`
}

// dispatchTaskToDevices 使用一次JOIN读取全部目标，并批量更新下发状态。
func dispatchTaskToDevices(db *gorm.DB, task *models.Task) taskDispatchResult {
	var scriptParams, scriptContent string
	if task.Script != nil {
		scriptParams = task.Script.Params
		scriptContent = task.Script.Content
	}

	result := taskDispatchResult{}
	runID := newCommandID("run")
	timeoutSeconds := normalizeTaskTimeout(task.TimeoutSeconds)
	const dispatchBatchSize = 100
	var cursor uint64
	for {
		var targets []taskDispatchTarget
		query := db.Table("task_devices AS td").
			Select("td.id AS task_device_id, d.device_id, d.device_params").
			Joins("JOIN devices AS d ON d.id = td.device_id").
			Where("td.task_id = ? AND td.id > ?", task.ID, cursor).
			Order("td.id ASC").Limit(dispatchBatchSize)
		if err := query.Find(&targets).Error; err != nil || len(targets) == 0 {
			break
		}
		targetIDs := make([]uint64, 0, len(targets))
		for _, target := range targets {
			targetIDs = append(targetIDs, target.TaskDeviceID)
		}
		if err := db.Model(&models.TaskDevice{}).Where("id IN ?", targetIDs).Updates(map[string]interface{}{
			"run_id": runID, "status": 0, "result": "", "started_at": nil, "deadline_at": nil, "finished_at": nil,
		}).Error; err != nil {
			result.Busy += len(targets)
			continue
		}
		deliveredIDs := make([]uint64, 0, len(targets))
		for _, target := range targets {
			cursor = target.TaskDeviceID
			if Hub == nil {
				result.Offline++
				continue
			}
			msg := WSMessage{Type: "task", Data: gin.H{
				"cmd_id": newCommandID("task"), "task_id": task.ID, "task_name": task.Name,
				"run_id": runID, "script": scriptContent, "params": MergeParams(scriptParams, task.Params, target.DeviceParams),
				"timeout_seconds": timeoutSeconds, "protocol_version": 2,
			}}
			err := Hub.SendCommandWithDelivery(target.DeviceID, msg, task.ID)
			switch {
			case err == nil:
				result.Sent++
				deliveredIDs = append(deliveredIDs, target.TaskDeviceID)
			case errors.Is(err, ErrDeviceOffline):
				result.Offline++
				if reliableDeliveryEnabled() {
					result.Queued++
				}
			case errors.Is(err, ErrSendQueueFull):
				result.Busy++
				if reliableDeliveryEnabled() {
					result.Queued++
				}
			default:
				result.Busy++
			}
		}
		if len(deliveredIDs) > 0 {
			now := time.Now()
			deadline := now.Add(time.Duration(timeoutSeconds) * time.Second)
			db.Model(&models.TaskDevice{}).Where("id IN ?", deliveredIDs).
				Updates(map[string]interface{}{"status": 1, "started_at": now, "deadline_at": deadline})
		}
		if len(targets) < dispatchBatchSize {
			break
		}
		// Give device writers time to drain their per-connection queues before
		// materialising another wave of script payloads.
		time.Sleep(25 * time.Millisecond)
	}
	return result
}

// ListTasks 任务列表
func (h *TaskHandler) ListTasks(c *gin.Context) {
	var page models.PageRequest
	c.ShouldBindQuery(&page)
	if page.Page <= 0 {
		page.Page = 1
	}
	if page.Size <= 0 {
		page.Size = 10
	}
	if page.Size > 200 {
		page.Size = 200
	}

	userID := middleware.GetUserID(c)

	var total int64
	query := h.DB.Model(&models.Task{}).Preload("Script")

	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ? OR EXISTS (SELECT 1 FROM task_shares ts WHERE ts.task_id = tasks.id AND ts.to_user_id = ?)", userID, userID)
	}

	if page.Keyword != "" {
		kw := "%" + page.Keyword + "%"
		query = query.Where("name LIKE ? OR description LIKE ?", kw, kw)
	}
	if c.Query("status") != "" {
		query = query.Where("status = ?", page.Status)
	}

	query.Count(&total)

	var tasks []models.Task
	offset := (page.Page - 1) * page.Size
	query.Offset(offset).Limit(page.Size).Order("id DESC").Find(&tasks)

	c.JSON(200, models.PageResponse{
		Code:    200,
		Message: "成功",
		Data:    tasks,
		Total:   total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// CreateTask 创建任务
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req struct {
		models.Task
		DeviceIDs []uint64 `json:"device_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	userID := middleware.GetUserID(c)
	req.Task.Name = strings.TrimSpace(req.Task.Name)
	if req.Task.Name == "" || req.Task.ScriptID == 0 {
		c.JSON(200, models.Response{Code: 400, Message: "任务名称和脚本不能为空"})
		return
	}
	if len(req.Task.Name) > 255 || len(req.Task.Description) > 512 || len(req.Task.Params) > maxScriptParamsBytes {
		c.JSON(200, models.Response{Code: 400, Message: "任务名称、描述或参数超过限制"})
		return
	}
	if len(req.DeviceIDs) > taskDeviceLimit() {
		c.JSON(200, models.Response{Code: 400, Message: fmt.Sprintf("单个任务最多关联%d台设备", taskDeviceLimit())})
		return
	}
	if req.Task.Params != "" {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(req.Task.Params), &params); err != nil {
			c.JSON(200, models.Response{Code: 400, Message: "任务参数必须是合法JSON对象"})
			return
		}
	}
	if req.Task.CronEnabled {
		if strings.TrimSpace(req.Task.CronExpr) == "" || ValidateCronExpression(req.Task.CronExpr) != nil {
			c.JSON(200, models.Response{Code: 400, Message: "Cron表达式无效"})
			return
		}
	}
	if err := validateTaskTimeout(req.Task.TimeoutSeconds); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "任务超时时间必须在30秒到24小时之间"})
		return
	}
	timezone, err := normalizeCronTimezone(req.Task.CronTimezone)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "定时任务时区无效"})
		return
	}
	misfirePolicy, err := normalizeMisfirePolicy(req.Task.MisfirePolicy)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "补跑策略必须是 skip、run_once 或 latest"})
		return
	}
	req.Task.TimeoutSeconds = normalizeTaskTimeout(req.Task.TimeoutSeconds)
	req.Task.CronTimezone = timezone
	req.Task.MisfirePolicy = misfirePolicy
	var script models.Script
	if err := h.DB.First(&script, req.Task.ScriptID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "脚本不存在"})
		return
	}
	var scriptShared int64
	h.DB.Model(&models.ScriptShare{}).Where("script_id = ? AND to_user_id = ?", script.ID, userID).Count(&scriptShared)
	if script.UserID != userID && scriptShared == 0 && !middleware.IsSystemAdminUser(h.DB, userID) {
		c.JSON(200, models.Response{Code: 403, Message: "无权使用此脚本"})
		return
	}
	deviceIDs, err := deviceIDsForUser(h.DB, req.DeviceIDs, userID)
	if err != nil {
		c.JSON(200, models.Response{Code: 403, Message: "包含无权使用的设备"})
		return
	}
	req.DeviceIDs = deviceIDs
	req.Task.UserID = userID
	req.Task.ID = 0
	req.Task.IsShared = 0
	req.Task.LastRunAt = nil
	req.Task.Script = nil
	req.Task.Devices = nil
	req.Task.Status = 0 // 默认停止状态

	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&req.Task).Error; err != nil {
			return err
		}
		taskDevices := make([]models.TaskDevice, 0, len(req.DeviceIDs))
		for _, deviceID := range req.DeviceIDs {
			taskDevices = append(taskDevices, models.TaskDevice{TaskID: req.Task.ID, DeviceID: deviceID, Status: 0})
		}
		if len(taskDevices) > 0 {
			if err := tx.CreateInBatches(&taskDevices, 250).Error; err != nil {
				return err
			}
		}
		return tx.Create(&models.TaskLog{TaskID: req.Task.ID, LogType: "info", Message: "任务已创建"}).Error
	}); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "创建失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "任务创建成功", Data: req.Task})

	// 同步到定时调度器
	if Scheduler != nil {
		if req.Task.CronEnabled {
			Scheduler.AddTask(req.Task.ID, req.Task.CronExpr)
		} else {
			Scheduler.RemoveTask(req.Task.ID)
		}
	}
}

func taskDeviceLimit() int {
	limit := 2000
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CLOUD_TASK_MAX_DEVICES"))); err == nil && value >= 100 {
		limit = value
	}
	if limit > 10000 {
		limit = 10000
	}
	return limit
}

func cancelPendingTaskDeliveries(db *gorm.DB, taskIDs []uint64, reason string) {
	if db == nil || len(taskIDs) == 0 || !reliableDeliveryEnabled() {
		return
	}
	_ = db.Model(&models.CommandDelivery{}).
		Where("task_id IN ? AND message_type = ? AND status IN ?", taskIDs, "task", []string{deliveryQueued, deliverySent}).
		Updates(map[string]interface{}{"status": deliveryFailed, "last_error": reason}).Error
}

// UpdateTask 更新任务
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	id := c.Param("id")
	userID := middleware.GetUserID(c)
	var accessTask models.Task
	if err := h.DB.First(&accessTask, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "任务不存在"})
		return
	}
	if accessTask.UserID != userID && !middleware.IsSystemAdminUser(h.DB, userID) {
		c.JSON(200, models.Response{Code: 403, Message: "无权修改此任务"})
		return
	}

	var task models.Task
	if err := h.DB.First(&task, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "任务不存在"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	delete(updates, "id")
	delete(updates, "user_id")
	delete(updates, "created_at")
	delete(updates, "updated_at")
	delete(updates, "status")
	delete(updates, "is_shared")
	delete(updates, "last_run_at")
	delete(updates, "script_id")
	allowedUpdates := map[string]bool{
		"name": true, "description": true, "params": true,
		"cron_expr": true, "cron_enabled": true, "priority": true,
		"cron_timezone": true, "misfire_policy": true, "timeout_seconds": true,
	}
	for key := range updates {
		if !allowedUpdates[key] {
			delete(updates, key)
		}
	}
	if value, ok := updates["params"]; ok {
		params, isString := value.(string)
		if !isString {
			c.JSON(200, models.Response{Code: 400, Message: "任务参数格式无效"})
			return
		}
		if len(params) > maxScriptParamsBytes {
			c.JSON(200, models.Response{Code: 400, Message: "任务参数超过 64 KiB 限制"})
			return
		}
		if params != "" {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(params), &parsed); err != nil {
				c.JSON(200, models.Response{Code: 400, Message: "任务参数必须是合法JSON对象"})
				return
			}
		}
	}
	for key, maximum := range map[string]int{"name": 255, "description": 512, "cron_expr": 128} {
		if value, exists := updates[key]; exists {
			text, ok := value.(string)
			if !ok || len(text) > maximum || (key == "name" && strings.TrimSpace(text) == "") {
				c.JSON(200, models.Response{Code: 400, Message: "任务字段格式或长度无效"})
				return
			}
		}
	}
	if value, ok := updates["cron_enabled"]; ok {
		if _, valid := value.(bool); !valid {
			c.JSON(200, models.Response{Code: 400, Message: "cron_enabled 格式无效"})
			return
		}
	}
	if value, ok := updates["cron_expr"]; ok {
		if _, valid := value.(string); !valid {
			c.JSON(200, models.Response{Code: 400, Message: "cron_expr 格式无效"})
			return
		}
	}
	if value, ok := updates["timeout_seconds"]; ok {
		number, valid := toFloat(value)
		if !valid || number != float64(int(number)) || validateTaskTimeout(int(number)) != nil {
			c.JSON(200, models.Response{Code: 400, Message: "任务超时时间必须在30秒到24小时之间"})
			return
		}
		updates["timeout_seconds"] = int(number)
	}
	if value, ok := updates["cron_timezone"]; ok {
		text, valid := value.(string)
		timezone, err := normalizeCronTimezone(text)
		if !valid || err != nil {
			c.JSON(200, models.Response{Code: 400, Message: "定时任务时区无效"})
			return
		}
		updates["cron_timezone"] = timezone
	}
	if value, ok := updates["misfire_policy"]; ok {
		text, valid := value.(string)
		policy, err := normalizeMisfirePolicy(text)
		if !valid || err != nil {
			c.JSON(200, models.Response{Code: 400, Message: "补跑策略必须是 skip、run_once 或 latest"})
			return
		}
		updates["misfire_policy"] = policy
	}
	enabled := task.CronEnabled
	if value, ok := updates["cron_enabled"].(bool); ok {
		enabled = value
	}
	expr := task.CronExpr
	if value, ok := updates["cron_expr"].(string); ok {
		expr = strings.TrimSpace(value)
	}
	if enabled && (expr == "" || ValidateCronExpression(expr) != nil) {
		c.JSON(200, models.Response{Code: 400, Message: "Cron表达式无效"})
		return
	}

	if err := h.DB.Model(&task).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}

	// 同步调度器
	if Scheduler != nil {
		enabled := task.CronEnabled
		if ce, ok := updates["cron_enabled"].(bool); ok {
			enabled = ce
		}
		expr := task.CronExpr
		if value, ok := updates["cron_expr"].(string); ok {
			expr = value
		}
		if enabled {
			Scheduler.AddTask(task.ID, expr)
		} else {
			Scheduler.RemoveTask(task.ID)
		}
	}

	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

// DeleteTask 删除任务
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	id := c.Param("id")
	if !ensureTaskAccess(c, h.DB, id, true) {
		return
	}

	if Scheduler != nil {
		Scheduler.RemoveTask(strToUint64(id))
	}
	cancelPendingTaskDeliveries(h.DB, []uint64{strToUint64(id)}, "task deleted")
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", id).Delete(&models.TaskDevice{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", id).Delete(&models.TaskLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", id).Delete(&models.TaskShare{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Task{}, id).Error
	}); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "删除失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

// StartTask 启动任务（自动推送到所有关联设备）
func (h *TaskHandler) StartTask(c *gin.Context) {
	id := c.Param("id")
	if !ensureTaskAccess(c, h.DB, id, true) {
		return
	}

	var task models.Task
	if err := h.DB.Preload("Script").First(&task, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "任务不存在"})
		return
	}
	if task.Status == 1 {
		c.JSON(200, models.Response{Code: 409, Message: "任务正在运行，请勿重复启动"})
		return
	}

	if err := h.DB.Model(&task).Where("status <> ?", 1).Update("status", 1).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "任务状态更新失败"})
		return
	}

	h.DB.Create(&models.TaskLog{
		TaskID:  task.ID,
		LogType: "info",
		Message: "任务已启动",
	})

	if Scheduler == nil || !Scheduler.EnqueueTask(task.ID) {
		_ = h.DB.Model(&task).Update("status", 0).Error
		c.JSON(200, models.Response{Code: 503, Message: "任务派发队列繁忙，请稍后重试"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "任务已进入后台派发队列", Data: gin.H{"task_id": task.ID, "queued": true}})
}

// StopTask 停止任务
func (h *TaskHandler) StopTask(c *gin.Context) {
	id := c.Param("id")
	if !ensureTaskAccess(c, h.DB, id, true) {
		return
	}

	var task models.Task
	if err := h.DB.First(&task, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "任务不存在"})
		return
	}

	// 设备端执行无法被强制终止时，至少发送取消指令并保留服务端停止状态。
	cancelPendingTaskDeliveries(h.DB, []uint64{task.ID}, "task stopped by administrator")
	var targets []taskDispatchTarget
	h.DB.Table("task_devices AS td").Select("d.device_id, td.run_id").Joins("JOIN devices AS d ON d.id = td.device_id").Where("td.task_id = ? AND td.status = 1", task.ID).Find(&targets)
	for _, target := range targets {
		_ = Hub.SendCommandWithDelivery(target.DeviceID, WSMessage{Type: "command", Data: gin.H{"cmd_id": newCommandID("cancel-task"), "command": "cancel_task", "task_id": task.ID, "run_id": target.RunID}}, task.ID)
	}
	if err := h.DB.Model(&task).Update("status", 0).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "任务停止失败"})
		return
	}
	// 将执行中的设备重置为待执行
	h.DB.Model(&models.TaskDevice{}).Where("task_id = ? AND status = 1", id).Updates(map[string]interface{}{
		"status": 4, "result": "任务已由管理员停止", "deadline_at": nil, "finished_at": time.Now(),
	})

	h.DB.Create(&models.TaskLog{
		TaskID:  task.ID,
		LogType: "info",
		Message: "任务已停止",
	})

	c.JSON(200, models.Response{Code: 200, Message: "任务已停止"})
}

// ResetTask 重置任务
func (h *TaskHandler) ResetTask(c *gin.Context) {
	id := c.Param("id")
	if !ensureTaskAccess(c, h.DB, id, true) {
		return
	}

	cancelPendingTaskDeliveries(h.DB, []uint64{strToUint64(id)}, "task reset by administrator")
	h.DB.Model(&models.Task{}).Where("id = ?", id).Update("status", 0)
	h.DB.Model(&models.TaskDevice{}).Where("task_id = ?", id).Updates(map[string]interface{}{
		"status":      0,
		"result":      "",
		"started_at":  nil,
		"deadline_at": nil,
		"finished_at": nil,
	})

	h.DB.Create(&models.TaskLog{
		TaskID:  strToUint64(id),
		LogType: "info",
		Message: "任务已重置",
	})

	c.JSON(200, models.Response{Code: 200, Message: "任务已重置"})
}

// BatchControlTasks 批量控制任务 (启动/停止/重置)
func (h *TaskHandler) BatchControlTasks(c *gin.Context) {
	var req struct {
		IDs    []uint64 `json:"ids" binding:"required"`
		Action string   `json:"action" binding:"required"` // start/stop/reset
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	if len(req.IDs) == 0 || len(req.IDs) > 10000 {
		c.JSON(200, models.Response{Code: 400, Message: "任务数量无效"})
		return
	}
	switch req.Action {
	case "start", "stop", "reset":
	default:
		c.JSON(200, models.Response{Code: 400, Message: "不支持的任务操作"})
		return
	}
	userID := middleware.GetUserID(c)
	query := h.DB.Model(&models.Task{}).Where("id IN ?", req.IDs)
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ?", userID)
	}
	var allowedIDs []uint64
	if err := query.Pluck("id", &allowedIDs).Error; err != nil || len(allowedIDs) != len(uniqueUint64(req.IDs)) {
		c.JSON(200, models.Response{Code: 403, Message: "包含无权操作的任务"})
		return
	}
	req.IDs = allowedIDs

	switch req.Action {
	case "start":
		var tasks []models.Task
		if err := h.DB.Preload("Script").Where("id IN ?", req.IDs).Find(&tasks).Error; err != nil {
			c.JSON(200, models.Response{Code: 500, Message: "读取任务失败"})
			return
		}
		queued := 0
		for i := range tasks {
			if tasks[i].Status == 1 {
				continue
			}
			if err := h.DB.Model(&tasks[i]).Update("status", 1).Error; err != nil {
				c.JSON(200, models.Response{Code: 500, Message: "任务启动失败"})
				return
			}
			if Scheduler != nil && Scheduler.EnqueueTask(tasks[i].ID) {
				queued++
			} else {
				_ = h.DB.Model(&tasks[i]).Update("status", 0).Error
			}
		}
		if queued == 0 && len(tasks) > 0 {
			c.JSON(200, models.Response{Code: 503, Message: "任务派发队列繁忙，请稍后重试"})
			return
		}
	case "stop":
		cancelPendingTaskDeliveries(h.DB, req.IDs, "task batch-stopped by administrator")
		var targets []taskDispatchTarget
		h.DB.Table("task_devices AS td").Select("td.task_id, d.device_id, td.run_id").
			Joins("JOIN devices AS d ON d.id = td.device_id").
			Where("td.task_id IN ? AND td.status = 1", req.IDs).Find(&targets)
		for _, target := range targets {
			if Hub != nil {
				_ = Hub.SendCommandWithDelivery(target.DeviceID, WSMessage{Type: "command", Data: gin.H{
					"cmd_id": newCommandID("cancel-task"), "command": "cancel_task", "task_id": target.TaskID, "run_id": target.RunID,
				}}, target.TaskID)
			}
		}
		h.DB.Model(&models.Task{}).Where("id IN ?", req.IDs).Update("status", 0)
		h.DB.Model(&models.TaskDevice{}).Where("task_id IN ? AND status = 1", req.IDs).Updates(map[string]interface{}{
			"status": 4, "result": "任务已由管理员批量停止", "deadline_at": nil, "finished_at": time.Now(),
		})
	case "reset":
		cancelPendingTaskDeliveries(h.DB, req.IDs, "task batch-reset by administrator")
		h.DB.Model(&models.Task{}).Where("id IN ?", req.IDs).Update("status", 0)
		h.DB.Model(&models.TaskDevice{}).Where("task_id IN ?", req.IDs).Updates(map[string]interface{}{
			"status": 0, "result": "", "started_at": nil, "deadline_at": nil, "finished_at": nil,
		})
	}

	c.JSON(200, models.Response{Code: 200, Message: "批量操作成功"})
}

// RepairTask 修复任务 - 重新下发异常设备
func (h *TaskHandler) RepairTask(c *gin.Context) {
	id := c.Param("id")
	if !ensureTaskAccess(c, h.DB, id, true) {
		return
	}

	var req models.TaskRepairRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	taskID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的任务ID"})
		return
	}

	if len(req.DeviceIDs) == 0 || len(req.DeviceIDs) > taskDeviceLimit() {
		c.JSON(200, models.Response{Code: 400, Message: "修复设备数量无效"})
		return
	}
	var task models.Task
	if err := h.DB.Preload("Script").First(&task, taskID).Error; err != nil || task.Script == nil {
		c.JSON(200, models.Response{Code: 404, Message: "任务或脚本不存在"})
		return
	}
	var targets []taskDispatchTarget
	if err := h.DB.Table("task_devices AS td").
		Select("td.id AS task_device_id, td.task_id, d.device_id, d.device_params").
		Joins("JOIN devices AS d ON d.id = td.device_id").
		Where("td.task_id = ? AND td.device_id IN ?", taskID, uniqueUint64(req.DeviceIDs)).Find(&targets).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "读取修复设备失败"})
		return
	}
	if len(targets) == 0 {
		c.JSON(200, models.Response{Code: 404, Message: "没有匹配的任务设备"})
		return
	}
	runID := newCommandID("repair-run")
	timeoutSeconds := normalizeTaskTimeout(task.TimeoutSeconds)
	result := taskDispatchResult{}
	for _, target := range targets {
		_ = h.DB.Model(&models.TaskDevice{}).Where("id = ?", target.TaskDeviceID).Updates(map[string]interface{}{
			"run_id": runID, "status": 0, "result": "", "started_at": nil, "deadline_at": nil, "finished_at": nil,
		}).Error
		message := WSMessage{Type: "task", Data: gin.H{
			"cmd_id": newCommandID("repair-task"), "task_id": task.ID, "task_name": task.Name,
			"run_id": runID, "script": task.Script.Content,
			"params":          MergeParams(task.Script.Params, task.Params, target.DeviceParams),
			"timeout_seconds": timeoutSeconds, "protocol_version": 2,
		}}
		var err error
		if Hub == nil {
			err = ErrDeviceOffline
		} else {
			err = Hub.SendCommandWithDelivery(target.DeviceID, message, task.ID)
		}
		switch {
		case err == nil:
			result.Sent++
			now := time.Now()
			deadline := now.Add(time.Duration(timeoutSeconds) * time.Second)
			_ = h.DB.Model(&models.TaskDevice{}).Where("id = ?", target.TaskDeviceID).
				Updates(map[string]interface{}{"status": 1, "started_at": now, "deadline_at": deadline}).Error
		case errors.Is(err, ErrDeviceOffline), errors.Is(err, ErrSendQueueFull):
			result.Offline++
			if reliableDeliveryEnabled() && Hub != nil {
				result.Queued++
			}
		default:
			result.Busy++
		}
	}
	if result.Sent > 0 {
		_ = h.DB.Model(&task).Update("status", 1).Error
	}
	message := fmt.Sprintf("任务修复派发完成: sent=%d queued=%d failed=%d", result.Sent, result.Queued, result.Busy)
	_ = h.DB.Create(&models.TaskLog{TaskID: taskID, LogType: "info", Message: message}).Error
	c.JSON(200, models.Response{Code: 200, Message: "任务修复已重新派发", Data: result})
}

// GetTaskDevices 获取任务关联设备列表 (含状态)
func (h *TaskHandler) GetTaskDevices(c *gin.Context) {
	id := c.Param("id")
	if !ensureTaskAccess(c, h.DB, id, false) {
		return
	}

	var page models.PageRequest
	_ = c.ShouldBindQuery(&page)
	page.Normalize(50, 200)
	query := h.DB.Model(&models.TaskDevice{}).Where("task_id = ?", id)
	if c.Query("status") != "" {
		query = query.Where("status = ?", page.Status)
	}
	var total int64
	query.Count(&total)
	var taskDevices []models.TaskDevice
	query.Preload("Device").Order("id ASC").Offset((page.Page - 1) * page.Size).Limit(page.Size).Find(&taskDevices)
	c.JSON(200, models.PageResponse{Code: 200, Message: "成功", Data: taskDevices, Total: total, Page: page.Page, Size: page.Size})
}

// GetTaskLogs 获取任务日志
func (h *TaskHandler) GetTaskLogs(c *gin.Context) {
	var page models.PageRequest
	c.ShouldBindQuery(&page)
	if page.Page <= 0 {
		page.Page = 1
	}
	if page.Size <= 0 {
		page.Size = 20
	}
	if page.Size > 200 {
		page.Size = 200
	}

	id := c.Param("id")
	if !ensureTaskAccess(c, h.DB, id, false) {
		return
	}

	var total int64
	query := h.DB.Model(&models.TaskLog{}).Where("task_id = ?", id)
	query.Count(&total)

	var logs []models.TaskLog
	offset := (page.Page - 1) * page.Size
	query.Offset(offset).Limit(page.Size).Order("id DESC").Find(&logs)

	c.JSON(200, models.PageResponse{
		Code:    200,
		Message: "成功",
		Data:    logs,
		Total:   total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// ShareTask 共享任务
func (h *TaskHandler) ShareTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的任务ID"})
		return
	}

	var req struct {
		ToUserIDs []uint64 `json:"to_user_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	if len(req.ToUserIDs) == 0 || len(req.ToUserIDs) > 1000 {
		c.JSON(200, models.Response{Code: 400, Message: "共享用户数量无效"})
		return
	}

	userID := middleware.GetUserID(c)
	task, accessErr := loadTaskForUser(h.DB, id, userID, true, false)
	if accessErr != nil {
		c.JSON(200, models.Response{Code: 403, Message: "无权分享此任务"})
		return
	}

	for _, toUserID := range req.ToUserIDs {
		var target models.User
		if h.DB.Select("id").First(&target, toUserID).Error != nil {
			c.JSON(200, models.Response{Code: 400, Message: "目标用户不存在"})
			return
		}
		share := models.TaskShare{
			TaskID:     id,
			FromUserID: userID,
			ToUserID:   toUserID,
		}
		if err := h.DB.Where(share).FirstOrCreate(&share).Error; err != nil {
			c.JSON(200, models.Response{Code: 500, Message: "共享失败"})
			return
		}
	}

	if err := h.DB.Model(task).Update("is_shared", 1).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "共享状态保存失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "共享成功"})
}

// ListTaskShares 任务共享列表
func (h *TaskHandler) ListTaskShares(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var shares []struct {
		models.TaskShare
		TaskName string `json:"task_name"`
		FromUser string `json:"from_user"`
	}

	h.DB.Table("task_shares ts").
		Select("ts.*, t.name as task_name, u.username as from_user").
		Joins("JOIN tasks t ON t.id = ts.task_id").
		Joins("JOIN users u ON u.id = ts.from_user_id").
		Where("ts.to_user_id = ?", userID).
		Scan(&shares)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: shares})
}

// ============================================
// 参数合并逻辑 (设备 > 任务 > 脚本)
// ============================================

// MergeParams 按优先级合并参数
func MergeParams(scriptParams, taskParams, deviceParams string) map[string]interface{} {
	result := make(map[string]interface{})

	// 1. 先取脚本参数
	if scriptParams != "" {
		json.Unmarshal([]byte(scriptParams), &result)
	}

	// 2. 覆盖任务参数
	if taskParams != "" {
		var tp map[string]interface{}
		if json.Unmarshal([]byte(taskParams), &tp) == nil {
			for k, v := range tp {
				result[k] = v
			}
		}
	}

	// 3. 最终覆盖设备参数（最高优先级）
	if deviceParams != "" {
		var dp map[string]interface{}
		if json.Unmarshal([]byte(deviceParams), &dp) == nil {
			for k, v := range dp {
				result[k] = v
			}
		}
	}

	return result
}

// GetMergedTaskParams 获取任务下某设备的合并参数
func (h *TaskHandler) GetMergedTaskParams(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的任务ID"})
		return
	}
	if !ensureTaskAccess(c, h.DB, c.Param("id"), false) {
		return
	}
	deviceID, err := strconv.ParseUint(c.Query("device_id"), 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的设备ID"})
		return
	}

	var task models.Task
	if err := h.DB.First(&task, taskID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "任务不存在"})
		return
	}

	var device models.Device
	if err := h.DB.First(&device, deviceID).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "设备不存在"})
		return
	}
	var relation models.TaskDevice
	if err := h.DB.Where("task_id = ? AND device_id = ?", taskID, deviceID).First(&relation).Error; err != nil {
		c.JSON(200, models.Response{Code: 403, Message: "该设备未关联此任务"})
		return
	}

	// 获取脚本参数
	var scriptParams string
	if task.ScriptID > 0 {
		var script models.Script
		h.DB.Select("params").First(&script, task.ScriptID)
		scriptParams = script.Params
	}

	merged := MergeParams(scriptParams, task.Params, device.DeviceParams)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: merged})
}

func NewTaskHandler(db *gorm.DB) *TaskHandler {
	return &TaskHandler{DB: db}
}

func strToUint64(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// 初始化models需要的类型
var _ = time.Now
