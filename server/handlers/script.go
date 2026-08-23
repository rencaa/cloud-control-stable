package handlers

import (
	"strconv"
	"strings"

	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ScriptHandler struct {
	DB *gorm.DB
}

const (
	maxScriptContentBytes = 512 * 1024
	maxScriptParamsBytes  = 64 * 1024
)

// ListScripts 脚本列表
func (h *ScriptHandler) ListScripts(c *gin.Context) {
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
	query := h.DB.Model(&models.Script{}).Select("id", "name", "description", "filename", "params", "user_id", "is_shared", "download_count", "created_at", "updated_at")

	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ? OR EXISTS (SELECT 1 FROM script_shares ss WHERE ss.script_id = scripts.id AND ss.to_user_id = ?)", userID, userID)
	}

	if page.Keyword != "" {
		kw := "%" + page.Keyword + "%"
		query = query.Where("name LIKE ? OR description LIKE ?", kw, kw)
	}

	query.Count(&total)

	var scripts []models.Script
	offset := (page.Page - 1) * page.Size
	query.Offset(offset).Limit(page.Size).Order("id DESC").Find(&scripts)

	c.JSON(200, models.PageResponse{
		Code:    200,
		Message: "成功",
		Data:    scripts,
		Total:   total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// CreateScript 上传/创建脚本
func (h *ScriptHandler) CreateScript(c *gin.Context) {
	var script models.Script
	if err := c.ShouldBindJSON(&script); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}
	script.Name = strings.TrimSpace(script.Name)
	script.Filename = strings.TrimSpace(script.Filename)
	if script.Name == "" || len(script.Name) > 255 || len(script.Filename) > 255 || len(script.Content) > maxScriptContentBytes || len(script.Params) > maxScriptParamsBytes {
		c.JSON(200, models.Response{Code: 400, Message: "脚本名称、文件名、内容或参数超过限制"})
		return
	}

	script.UserID = middleware.GetUserID(c)
	script.ID = 0
	script.IsShared = 0
	script.DownloadCount = 0

	if err := h.DB.Create(&script).Error; err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "创建失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "脚本创建成功", Data: script})
}

// UpdateScript 更新脚本
func (h *ScriptHandler) UpdateScript(c *gin.Context) {
	id := c.Param("id")

	var script models.Script
	if err := h.DB.First(&script, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "脚本不存在"})
		return
	}

	// 检查权限
	userID := middleware.GetUserID(c)
	if script.UserID != userID {
		c.JSON(200, models.Response{Code: 403, Message: "无权限修改此脚本"})
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
	delete(updates, "is_shared")
	delete(updates, "download_count")
	allowed := map[string]bool{"name": true, "description": true, "filename": true, "content": true, "params": true}
	for key := range updates {
		if !allowed[key] {
			delete(updates, key)
		}
	}
	for key, maximum := range map[string]int{"name": 255, "filename": 255, "content": maxScriptContentBytes, "params": maxScriptParamsBytes, "description": 512} {
		if value, exists := updates[key]; exists {
			text, ok := value.(string)
			if !ok || len(text) > maximum || (key == "name" && strings.TrimSpace(text) == "") {
				c.JSON(200, models.Response{Code: 400, Message: "脚本字段格式或长度无效"})
				return
			}
			if key == "name" || key == "filename" {
				text = strings.TrimSpace(text)
			}
			updates[key] = text
		}
	}

	if err := h.DB.Model(&script).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

// DeleteScript 删除脚本
func (h *ScriptHandler) DeleteScript(c *gin.Context) {
	id := c.Param("id")

	var script models.Script
	if err := h.DB.First(&script, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "脚本不存在"})
		return
	}

	userID := middleware.GetUserID(c)
	if script.UserID != userID {
		c.JSON(200, models.Response{Code: 403, Message: "无权限删除此脚本"})
		return
	}

	if err := h.DB.Delete(&script).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "删除失败"})
		return
	}
	// 清理关联
	h.DB.Where("script_id = ?", id).Delete(&models.ScriptShare{})

	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

// GetScriptContent 获取脚本内容 (用于在线编辑)
func (h *ScriptHandler) GetScriptContent(c *gin.Context) {
	id := c.Param("id")

	var script models.Script
	if err := h.DB.First(&script, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "脚本不存在"})
		return
	}
	userID := middleware.GetUserID(c)
	var shared int64
	h.DB.Model(&models.ScriptShare{}).Where("script_id = ? AND to_user_id = ?", script.ID, userID).Count(&shared)
	if script.UserID != userID && shared == 0 && !middleware.IsSystemAdminUser(h.DB, userID) {
		c.JSON(200, models.Response{Code: 403, Message: "无权查看此脚本"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: gin.H{
		"id":       script.ID,
		"name":     script.Name,
		"filename": script.Filename,
		"content":  script.Content,
		"params":   script.Params,
	}})
}

// ShareScript 共享脚本
func (h *ScriptHandler) ShareScript(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "无效的脚本ID"})
		return
	}
	var script models.Script
	if err := h.DB.First(&script, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "脚本不存在"})
		return
	}
	userID := middleware.GetUserID(c)
	if script.UserID != userID && !middleware.IsSystemAdminUser(h.DB, userID) {
		c.JSON(200, models.Response{Code: 403, Message: "无权分享此脚本"})
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

	for _, toUserID := range req.ToUserIDs {
		var target models.User
		if h.DB.Select("id").First(&target, toUserID).Error != nil {
			c.JSON(200, models.Response{Code: 400, Message: "目标用户不存在"})
			return
		}
		share := models.ScriptShare{
			ScriptID:   id,
			FromUserID: userID,
			ToUserID:   toUserID,
		}
		if err := h.DB.Where(share).FirstOrCreate(&share).Error; err != nil {
			c.JSON(200, models.Response{Code: 500, Message: "共享失败"})
			return
		}
	}

	// 标记脚本为共享
	if err := h.DB.Model(&models.Script{}).Where("id = ?", id).Update("is_shared", 1).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "共享状态保存失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "共享成功"})
}

// ListScriptShares 共享列表
func (h *ScriptHandler) ListScriptShares(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var shares []struct {
		models.ScriptShare
		ScriptName string `json:"script_name"`
		FromUser   string `json:"from_user"`
	}

	h.DB.Table("script_shares ss").
		Select("ss.*, s.name as script_name, u.username as from_user").
		Joins("JOIN scripts s ON s.id = ss.script_id").
		Joins("JOIN users u ON u.id = ss.from_user_id").
		Where("ss.to_user_id = ?", userID).
		Scan(&shares)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: shares})
}

func NewScriptHandler(db *gorm.DB) *ScriptHandler {
	return &ScriptHandler{DB: db}
}

// ScriptShare 脚本共享复用models中的定义
type ScriptShare = models.ScriptShare
