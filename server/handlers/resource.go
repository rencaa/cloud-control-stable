package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ResourceHandler struct {
	DB *gorm.DB
}

var resourceQuotaMu sync.Mutex

// ListResources 资源列表
func (h *ResourceHandler) ListResources(c *gin.Context) {
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
	query := h.DB.Model(&models.Resource{})
	if !middleware.IsSystemAdminUser(h.DB, userID) {
		query = query.Where("user_id = ? OR EXISTS (SELECT 1 FROM resource_shares rs WHERE rs.resource_id = resources.id AND rs.to_user_id = ?)", userID, userID)
	}

	if page.Keyword != "" {
		kw := "%" + page.Keyword + "%"
		query = query.Where("name LIKE ? OR filename LIKE ?", kw, kw)
	}
	query.Count(&total)

	var resources []models.Resource
	offset := (page.Page - 1) * page.Size
	query.Offset(offset).Limit(page.Size).Order("id DESC").Find(&resources)

	c.JSON(200, models.PageResponse{
		Code:    200,
		Message: "成功",
		Data:    resources,
		Total:   total,
		Page:    page.Page,
		Size:    page.Size,
	})
}

// UploadResource 上传资源
func (h *ResourceHandler) UploadResource(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.App.Upload.MaxSize+1<<20)
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "文件上传失败"})
		return
	}

	// 检查大小
	if file.Size > config.App.Upload.MaxSize {
		c.JSON(200, models.Response{Code: 400, Message: "文件大小超过限制"})
		return
	}
	resourceQuotaMu.Lock()
	defer resourceQuotaMu.Unlock()
	if !resourceStorageAvailable(h.DB, file.Size, 0) {
		c.JSON(200, models.Response{Code: 507, Message: "资源存储配额不足，请先删除旧文件"})
		return
	}

	savePath, err := newResourceStoragePath(config.App.Upload.UploadPath, file.Filename)
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "文件路径生成失败"})
		return
	}
	if err := writeUploadedResource(file, savePath); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "文件提交失败"})
		return
	}

	// 保存到数据库
	resource := models.Resource{
		Name:     c.PostForm("name"),
		Filename: file.Filename,
		FilePath: savePath,
		FileSize: file.Size,
		MimeType: file.Header.Get("Content-Type"),
		UserID:   middleware.GetUserID(c),
	}

	if resource.Name == "" {
		resource.Name = file.Filename
	}

	if err := h.DB.Create(&resource).Error; err != nil {
		_ = os.Remove(savePath)
		c.JSON(200, models.Response{Code: 500, Message: "资源记录保存失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "上传成功", Data: resource})
}

// DeleteResource 删除资源
func (h *ResourceHandler) DeleteResource(c *gin.Context) {
	id := c.Param("id")
	if !ensureResourceAccess(c, h.DB, id, true) {
		return
	}

	var resource models.Resource
	if err := h.DB.First(&resource, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "资源不存在"})
		return
	}

	quarantinePath := ""
	if resource.FilePath != "" {
		var err error
		quarantinePath, err = newResourceSidecarPath(resource.FilePath, "deleting")
		if err != nil {
			c.JSON(200, models.Response{Code: 500, Message: "删除准备失败"})
			return
		}
		if err := os.Rename(resource.FilePath, quarantinePath); err != nil {
			if !os.IsNotExist(err) {
				c.JSON(200, models.Response{Code: 500, Message: "资源文件暂时无法删除"})
				return
			}
			quarantinePath = ""
		}
	}
	if err := h.DB.Delete(&resource).Error; err != nil {
		if quarantinePath != "" {
			if restoreErr := os.Rename(quarantinePath, resource.FilePath); restoreErr != nil {
				log.Printf("resource %d database delete failed and file restore failed: %v", resource.ID, restoreErr)
			}
		}
		c.JSON(200, models.Response{Code: 500, Message: "资源记录删除失败"})
		return
	}
	if quarantinePath != "" {
		if err := os.Remove(quarantinePath); err != nil && !os.IsNotExist(err) {
			log.Printf("resource %d deleted but quarantined file cleanup failed: %v", resource.ID, err)
		}
	}
	c.JSON(200, models.Response{Code: 200, Message: "删除成功"})
}

// DownloadResource 通过ID下载
func (h *ResourceHandler) DownloadResource(c *gin.Context) {
	id := c.Param("id")
	if !ensureResourceAccess(c, h.DB, id, false) {
		return
	}

	var resource models.Resource
	if err := h.DB.First(&resource, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "资源不存在"})
		return
	}
	h.DB.Model(&resource).UpdateColumn("download_count", gorm.Expr("download_count + 1"))
	c.FileAttachment(resource.FilePath, resource.Filename)
}

// DownloadResourceByName 通过文件名下载（公开接口，给设备用）
func (h *ResourceHandler) DownloadResourceByName(c *gin.Context) {
	c.JSON(200, models.Response{Code: 410, Message: "该下载方式已停用，请使用资源ID下载"})
}

// ReplaceResource 替换资源
func (h *ResourceHandler) ReplaceResource(c *gin.Context) {
	id := c.Param("id")
	if !ensureResourceAccess(c, h.DB, id, true) {
		return
	}

	var resource models.Resource
	if err := h.DB.First(&resource, id).Error; err != nil {
		c.JSON(200, models.Response{Code: 404, Message: "资源不存在"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.App.Upload.MaxSize+1<<20)
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "文件上传失败"})
		return
	}
	if file.Size > config.App.Upload.MaxSize {
		c.JSON(200, models.Response{Code: 400, Message: "文件大小超过限制"})
		return
	}
	resourceQuotaMu.Lock()
	defer resourceQuotaMu.Unlock()
	if !resourceStorageAvailable(h.DB, file.Size, resource.FileSize) {
		c.JSON(200, models.Response{Code: 507, Message: "资源存储配额不足，请先删除旧文件"})
		return
	}

	// 不覆盖原文件：先将新内容写入新路径，再让数据库一次性切换引用。
	// 这样无论文件写入还是数据库更新失败，已发布资源都保持可下载。
	savePath, err := newResourceStoragePath(config.App.Upload.UploadPath, file.Filename)
	if err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "文件路径生成失败"})
		return
	}
	if err := writeUploadedResource(file, savePath); err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "文件提交失败"})
		return
	}

	oldPath := resource.FilePath
	if err := h.DB.Model(&resource).Updates(map[string]interface{}{
		"filename":  file.Filename,
		"file_size": file.Size,
		"mime_type": file.Header.Get("Content-Type"),
		"file_path": savePath,
	}).Error; err != nil {
		_ = os.Remove(savePath)
		c.JSON(200, models.Response{Code: 500, Message: "资源记录更新失败"})
		return
	}
	if oldPath != "" && oldPath != savePath {
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			// 数据库已安全切换到新文件，旧文件清理由后台或管理员后续处理。
			log.Printf("resource %d replaced but old file cleanup failed: %v", resource.ID, err)
		}
	}

	c.JSON(200, models.Response{Code: 200, Message: "替换成功"})
}

// ShareResource 共享资源
func (h *ResourceHandler) ShareResource(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || !ensureResourceAccess(c, h.DB, c.Param("id"), true) {
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

	for _, toUserID := range req.ToUserIDs {
		var target models.User
		if h.DB.Select("id").First(&target, toUserID).Error != nil {
			c.JSON(200, models.Response{Code: 400, Message: "目标用户不存在"})
			return
		}
		share := models.ResourceShare{
			ResourceID: id,
			FromUserID: userID,
			ToUserID:   toUserID,
		}
		if err := h.DB.Where(share).FirstOrCreate(&share).Error; err != nil {
			c.JSON(200, models.Response{Code: 500, Message: "共享失败"})
			return
		}
	}

	if err := h.DB.Model(&models.Resource{}).Where("id = ?", id).Update("is_shared", 1).Error; err != nil {
		c.JSON(200, models.Response{Code: 500, Message: "共享状态保存失败"})
		return
	}

	c.JSON(200, models.Response{Code: 200, Message: "共享成功"})
}

// ListResourceShares 资源共享列表
func (h *ResourceHandler) ListResourceShares(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var shares []struct {
		models.ResourceShare
		ResourceName string `json:"resource_name"`
		FromUser     string `json:"from_user"`
	}

	h.DB.Table("resource_shares rs").
		Select("rs.*, r.name as resource_name, u.username as from_user").
		Joins("JOIN resources r ON r.id = rs.resource_id").
		Joins("JOIN users u ON u.id = rs.from_user_id").
		Where("rs.to_user_id = ?", userID).
		Scan(&shares)

	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: shares})
}

func newResourceStoragePath(uploadPath, filename string) (string, error) {
	seed := make([]byte, 12)
	if _, err := rand.Read(seed); err != nil {
		return "", err
	}
	ext := filepath.Ext(filepath.Base(filename))
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(seed) + ext
	return filepath.Join(uploadPath, time.Now().Format("2006/01"), name), nil
}

func newResourceSidecarPath(path, purpose string) (string, error) {
	seed := make([]byte, 12)
	if _, err := rand.Read(seed); err != nil {
		return "", err
	}
	return path + "." + purpose + "-" + hex.EncodeToString(seed), nil
}

// writeUploadedResource writes through a private temporary file and fsyncs it
// before publishing. The destination must be unique and live on the same
// filesystem as its temporary sibling.
func writeUploadedResource(file interface {
	Open() (multipart.File, error)
}, destination string) (err error) {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	temporary := destination + ".uploading"
	defer func() {
		if err != nil {
			_ = os.Remove(temporary)
		}
	}()
	dst, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(dst, src); err == nil {
		err = dst.Sync()
	}
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, destination)
}

func resourceStorageAvailable(db *gorm.DB, incomingBytes, replacedBytes int64) bool {
	if db == nil || incomingBytes < 0 {
		return false
	}
	if config.App == nil || config.App.Upload.MaxTotalBytes <= 0 {
		_, _, writable := storageWatermark(".", incomingBytes)
		return writable
	}
	if _, _, writable := storageWatermark(config.App.Upload.UploadPath, incomingBytes); !writable {
		return false
	}
	var used int64
	if err := db.Model(&models.Resource{}).Select("COALESCE(SUM(file_size), 0)").Scan(&used).Error; err != nil {
		return false
	}
	projected := used - replacedBytes + incomingBytes
	return projected >= 0 && projected <= config.App.Upload.MaxTotalBytes
}

func NewResourceHandler(db *gorm.DB) *ResourceHandler {
	return &ResourceHandler{DB: db}
}
