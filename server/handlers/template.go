package handlers

import (
	"cloud-control-server/middleware"
	"cloud-control-server/models"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TemplateHandler struct {
	DB *gorm.DB
}

// ListTemplates 参数模板列表
func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	templateType := c.Query("type") // script/task/device

	var templates []models.ParameterTemplate
	query := h.DB.Model(&models.ParameterTemplate{})

	if templateType != "" {
		query = query.Where("type = ?", templateType)
	}

	userID := middleware.GetUserID(c)
	query = query.Where("user_id = ?", userID)
	query.Order("id DESC").Find(&templates)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: templates})
}

// CreateTemplate 创建参数模板
func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	var template models.ParameterTemplate
	if err := c.ShouldBindJSON(&template); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	template.UserID = middleware.GetUserID(c)

	if err := h.DB.Create(&template).Error; err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "创建失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "创建成功", Data: template})
}

// UpdateTemplate 更新参数模板
func (h *TemplateHandler) UpdateTemplate(c *gin.Context) {
	id := c.Param("id")

	var template models.ParameterTemplate
	if err := h.DB.First(&template, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "模板不存在"})
		return
	}
	userID := middleware.GetUserID(c)
	if template.UserID != userID && !middleware.IsSystemAdminUser(h.DB, userID) {
		c.JSON(200, models.Response{Code: 403, Message: "无权修改此模板"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "参数错误"})
		return
	}

	allowed := map[string]bool{"name": true, "type": true, "params": true, "description": true}
	for key := range updates {
		if !allowed[key] {
			delete(updates, key)
		}
	}
	if value, ok := updates["params"].(string); ok && !json.Valid([]byte(strings.TrimSpace(value))) {
		c.JSON(200, models.Response{Code: 400, Message: "params必须是合法JSON"})
		return
	}

	if err := h.DB.Model(&template).Updates(updates).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "更新失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "更新成功"})
}

// DeleteTemplate 删除参数模板
func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	id := c.Param("id")
	var template models.ParameterTemplate
	if err := h.DB.First(&template, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "模板不存在"})
		return
	}
	userID := middleware.GetUserID(c)
	if template.UserID != userID && !middleware.IsSystemAdminUser(h.DB, userID) {
		c.JSON(200, models.Response{Code: 403, Message: "无权删除此模板"})
		return
	}
	if err := h.DB.Delete(&template).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "删除失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

func NewTemplateHandler(db *gorm.DB) *TemplateHandler {
	return &TemplateHandler{DB: db}
}
