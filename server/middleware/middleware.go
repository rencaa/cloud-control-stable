package middleware

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var requestSequence atomic.Uint64

const defaultAPIRequestBodyLimit int64 = 4 << 20

// LimitAPIRequestBody bounds JSON and form API payloads before binders decode
// them into memory. Multipart uploads use their route-specific upload limit.
func LimitAPIRequestBody(limit int64) gin.HandlerFunc {
	if limit <= 0 {
		limit = defaultAPIRequestBodyLimit
	}
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions ||
			strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/") {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, models.Response{Code: http.StatusRequestEntityTooLarge, Message: "请求体超过限制"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// CORS 跨域中间件
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowedOrigin := ""
		origins := config.App.Security.CORSOrigins
		if len(origins) > 0 {
			for _, allowed := range origins {
				if origin == allowed {
					allowedOrigin = origin
					break
				}
			}
		}
		if allowedOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowedOrigin)
		}
		if allowedOrigin != "" {
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Device-Token, X-Request-ID")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Disposition")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = randomRequestID()
		}
		c.Header("X-Request-ID", requestID)

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()

		// 不记录请求体：登录密码、刷新 token、脚本内容和短信正文都可能在请求体中。
		log.Printf("[%s] %s %d %s request_id=%s ip=%s", method, path, statusCode, latency, requestID, c.ClientIP())
	}
}

func randomRequestID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z07:00") + "-" +
		strings.TrimSpace(os.Getenv("COMPUTERNAME")) + "-" +
		strconv.FormatUint(requestSequence.Add(1), 10)
}

// RBAC 权限检查中间件
func RBAC(db *gorm.DB, requiredPermissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := GetUserID(c)
		if userID == 0 {
			c.JSON(200, models.Response{Code: 401, Message: "未授权"})
			c.Abort()
			return
		}

		// 如果是 system_admin 则跳过权限检查
		var count int64
		db.Table("user_roles ur").
			Joins("JOIN roles r ON r.id = ur.role_id").
			Where("ur.user_id = ? AND r.code = ?", userID, "system_admin").
			Count(&count)

		if count > 0 {
			c.Next()
			return
		}

		// 检查具体权限
		for _, perm := range requiredPermissions {
			var permCount int64
			db.Table("role_permissions rp").
				Joins("JOIN permissions p ON p.id = rp.permission_id").
				Joins("JOIN user_roles ur ON ur.role_id = rp.role_id").
				Where("ur.user_id = ? AND p.code = ?", userID, perm).
				Count(&permCount)
			if permCount > 0 {
				c.Next()
				return
			}
		}

		c.JSON(200, models.Response{Code: 403, Message: "权限不足"})
		c.Abort()
	}
}

// SystemLogWriter 系统日志写入中间件
func SystemLogWriter(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 只记录写操作
		method := c.Request.Method
		if method == "GET" || method == "OPTIONS" {
			return
		}

		userID := GetUserID(c)
		username := GetUsername(c)

		sysLog := models.SystemLog{
			UserID:   userID,
			Username: username,
			Action:   method,
			Resource: c.Request.URL.Path,
			Detail:   method + " " + c.Request.URL.Path,
			IP:       c.ClientIP(),
		}
		db.Create(&sysLog)
	}
}

// AuditLogWriter moves routine audit inserts off the request path while keeping
// a synchronous fallback when the bounded queue is saturated.
type AuditLogWriter struct {
	db       *gorm.DB
	queue    chan models.SystemLog
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewAuditLogWriter(db *gorm.DB) *AuditLogWriter {
	w := &AuditLogWriter{db: db, queue: make(chan models.SystemLog, 256), stop: make(chan struct{})}
	w.wg.Add(1)
	go w.run()
	return w
}

func (w *AuditLogWriter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodOptions {
			return
		}
		entry := models.SystemLog{
			UserID: GetUserID(c), Username: GetUsername(c), Action: method,
			Resource: c.Request.URL.Path, Detail: method + " " + c.Request.URL.Path, IP: c.ClientIP(),
		}
		select {
		case w.queue <- entry:
		default:
			// Preserve audit records under pressure; queue saturation should be rare
			// because the worker commits them in batches.
			if err := w.db.Create(&entry).Error; err != nil {
				log.Printf("audit log fallback failed: %v", err)
			}
		}
	}
}

func (w *AuditLogWriter) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]models.SystemLog, 0, 50)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.db.CreateInBatches(&batch, 50).Error; err != nil {
			log.Printf("audit log batch failed: %v", err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case entry := <-w.queue:
			batch = append(batch, entry)
			if len(batch) >= cap(batch) {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.stop:
			for {
				select {
				case entry := <-w.queue:
					batch = append(batch, entry)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (w *AuditLogWriter) Stop(ctx context.Context) {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() { close(w.stop) })
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("audit log drain timed out with %d records queued", len(w.queue))
	}
}

// CacheControl 缓存控制（GET 请求加 max-age）
func CacheControl(maxAge string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" {
			c.Header("Cache-Control", "public, max-age="+maxAge)
		}
		c.Next()
	}
}
