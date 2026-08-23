package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/handlers"
	"cloud-control-server/middleware"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed web/dist
//go:embed web/dist/assets/_plugin*
var webFS embed.FS

const buildVersion = "v2026.08.24-optimized"

func main() {
	// 获取exe所在目录，确保相对路径正确
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	os.Chdir(exeDir)

	// 加载配置：兼容根目录config.json和旧版config/config.json。
	configPath := filepath.Join(exeDir, "config.json")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		configPath = filepath.Join(exeDir, "config", "config.json")
	}
	if err := config.Load(configPath); err != nil {
		log.Printf("config.json not found, using defaults")
	}
	// 支持SQLite和MySQL：SQLite保持单机默认，Docker大并发可切换MySQL。
	applyDatabaseEnvOverrides()
	if err := config.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	databaseDriver := strings.ToLower(strings.TrimSpace(config.App.Database.Driver))
	if databaseDriver == "" {
		databaseDriver = "sqlite"
	}
	gormConfig := &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn), // 生产环境降低日志级别
		SkipDefaultTransaction: true,                                // 禁用默认事务，减少锁竞争
		PrepareStmt:            true,                                // 预编译语句缓存
	}

	var db *gorm.DB
	var err error
	switch databaseDriver {
	case "mysql":
		db, err = gorm.Open(mysql.Open(config.App.Database.DSN()), gormConfig)
	default:
		dbPath := config.App.Database.DBName
		if dbPath == "" {
			dbPath = "cloud_control.db"
		}
		if !filepath.IsAbs(dbPath) {
			dbPath = filepath.Join(exeDir, dbPath)
		}
		_ = os.MkdirAll(filepath.Dir(dbPath), 0755)
		sqliteCacheKB := config.App.Database.SQLiteCacheKB
		if sqliteCacheKB < 1024 {
			sqliteCacheKB = 32768
		}
		sqliteMmapBytes := config.App.Database.SQLiteMmapBytes
		if sqliteMmapBytes < 1048576 {
			sqliteMmapBytes = 268435456
		}
		sqliteDSN := dbPath +
			"?_pragma=journal_mode(WAL)" +
			"&_pragma=busy_timeout(10000)" +
			"&_pragma=synchronous(NORMAL)" +
			"&_pragma=wal_autocheckpoint(1000)" +
			"&_pragma=journal_size_limit(67108864)" +
			fmt.Sprintf("&_pragma=cache_size(-%d)", sqliteCacheKB) +
			fmt.Sprintf("&_pragma=mmap_size(%d)", sqliteMmapBytes)
		db, err = gorm.Open(sqlite.Open(sqliteDSN), gormConfig)
		databaseDriver = "sqlite"
	}
	if err != nil {
		log.Fatalf("Failed to connect database: %v", err)
	}

	// 连接池配置：MySQL允许更多并发连接，SQLite保持适度连接数避免写锁竞争。
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		if databaseDriver == "mysql" {
			maxOpen, maxIdle := 64, 32
			if config.App.Database.MaxOpenConns > 0 {
				maxOpen = config.App.Database.MaxOpenConns
			}
			if config.App.Database.MaxIdleConns > 0 {
				maxIdle = config.App.Database.MaxIdleConns
			}
			sqlDB.SetMaxOpenConns(maxOpen)
			sqlDB.SetMaxIdleConns(maxIdle)
			sqlDB.SetConnMaxLifetime(30 * time.Minute)
			sqlDB.SetConnMaxIdleTime(5 * time.Minute)
		} else {
			maxOpen, maxIdle := 8, 8
			if config.App.Database.MaxOpenConns > 0 {
				maxOpen = config.App.Database.MaxOpenConns
			}
			if config.App.Database.MaxIdleConns > 0 {
				maxIdle = config.App.Database.MaxIdleConns
			}
			sqlDB.SetMaxOpenConns(maxOpen)
			sqlDB.SetMaxIdleConns(maxIdle)
			sqlDB.SetConnMaxLifetime(0)
			sqlDB.SetConnMaxIdleTime(0)
		}
	}
	log.Printf("Database driver: %s", databaseDriver)
	if databaseDriver == "sqlite" {
		var quickCheck string
		if err := db.Raw("PRAGMA quick_check").Scan(&quickCheck).Error; err != nil || !strings.EqualFold(strings.TrimSpace(quickCheck), "ok") {
			log.Fatalf("SQLite quick_check failed: result=%q error=%v", quickCheck, err)
		}
	}

	// 自动迁移表结构
	if err := db.AutoMigrate(
		&models.User{}, &models.Role{}, &models.Permission{},
		&models.UserRole{}, &models.RolePermission{},
		&models.DeviceGroup{}, &models.Device{}, &models.DeviceLog{}, &models.DeviceAutoRegistration{},
		&models.MQTTServiceAccount{},
		&models.Script{}, &models.ScriptShare{},
		&models.Task{}, &models.TaskDevice{}, &models.TaskLog{}, &models.TaskShare{},
		&models.CommandDelivery{},
		&models.ClientRelease{},
		&models.Resource{}, &models.ResourceShare{},
		&models.ParameterTemplate{},
		&models.DataTemplate{}, &models.DataRecord{}, &models.DataPermission{},
		&models.SystemLog{}, &models.SystemConfig{},
		&models.DeviceMetric{},
		&models.DeviceSms{}, &models.DeviceContact{},
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// 初始化管理员和默认数据
	handlers.InitAdminUser(db)
	if strings.TrimSpace(config.App.MQTT.BrokerURL) != "" {
		if err := handlers.EnsureMQTTServiceAccount(db, config.App.MQTT.Username, config.App.MQTT.Password); err != nil {
			log.Fatalf("Failed to provision MQTT bridge account: %v", err)
		}
	}

	// 启动WebSocket Hub
	wsHub := handlers.NewWSHub(db)
	go wsHub.Run()

	// 启动 MQTT Broker。MQTT 设备和旧 WebSocket 设备共用同一个 WSHub，
	// 任务/命令/心跳/截图等业务不需要维护两套下发逻辑。
	mqttPort := config.App.Server.MQTTPort
	if value := os.Getenv("CLOUD_MQTT_PORT"); value != "" {
		if port, parseErr := strconv.Atoi(value); parseErr == nil && port >= 0 {
			mqttPort = port
		}
	}
	var mqttBroker *handlers.MQTTBroker
	var externalMQTT *handlers.ExternalMQTTBridge
	if strings.TrimSpace(config.App.MQTT.BrokerURL) != "" {
		externalMQTT, err = handlers.NewExternalMQTTBridge(wsHub)
		if err != nil {
			log.Fatalf("Failed to configure external MQTT: %v", err)
		}
		if err := externalMQTT.Start(); err != nil {
			log.Fatalf("Failed to connect external MQTT: %v", err)
		}
		wsHub.SetExternalMQTT(externalMQTT)
		log.Printf("External MQTT bridge connected: %s", config.App.MQTT.BrokerURL)
	} else if mqttPort > 0 {
		mqttAddress := net.JoinHostPort(config.App.Server.MQTTBindAddress, strconv.Itoa(mqttPort))
		mqttListener, listenErr := net.Listen("tcp", mqttAddress)
		if listenErr != nil {
			log.Fatalf("MQTT listener required but unavailable on %s: %v", mqttAddress, listenErr)
		} else {
			mqttBroker = handlers.NewMQTTBroker(wsHub)
			go func() {
				log.Printf("MQTT broker listening on %s", mqttAddress)
				if err := mqttBroker.Serve(mqttListener); err != nil {
					log.Printf("MQTT broker stopped: %v", err)
				}
			}()
		}
	}

	// 启动定时任务调度器
	scheduler := handlers.NewTaskScheduler(db)
	go scheduler.Start()
	go scheduler.StartBackgroundJobs()

	// 确保上传目录存在（相对于exe目录）
	uploadPath := config.App.Upload.UploadPath
	if !filepath.IsAbs(uploadPath) {
		uploadPath = filepath.Join(exeDir, uploadPath)
	}
	os.MkdirAll(uploadPath, 0755)
	os.MkdirAll(filepath.Join(uploadPath, "screenshots"), 0755)

	// 初始化Gin（默认release模式）
	gin.SetMode(gin.ReleaseMode)
	if config.App.Server.Mode == "debug" {
		gin.SetMode(gin.DebugMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	// 只信任本机代理，避免外部请求伪造 X-Forwarded-For。
	if err := r.SetTrustedProxies(config.App.Security.TrustedProxyCIDRs); err != nil {
		log.Fatalf("invalid trusted proxy configuration: %v", err)
	}

	// 全局中间件
	r.Use(middleware.CORS())
	r.Use(middleware.LimitAPIRequestBody(4 << 20))
	r.Use(middleware.Logger())
	desktopShutdown := make(chan struct{}, 1)
	desktopShutdownToken := os.Getenv("CLOUD_DESKTOP_SHUTDOWN_TOKEN")
	if desktopShutdownToken != "" {
		r.POST("/internal/desktop-shutdown", func(c *gin.Context) {
			host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
			ip := net.ParseIP(host)
			token := c.GetHeader("X-Desktop-Shutdown-Token")
			if err != nil || ip == nil || !ip.IsLoopback() ||
				subtle.ConstantTimeCompare([]byte(token), []byte(desktopShutdownToken)) != 1 {
				c.JSON(http.StatusForbidden, gin.H{"status": "forbidden"})
				return
			}
			c.JSON(http.StatusAccepted, gin.H{"status": "shutting_down"})
			select {
			case desktopShutdown <- struct{}{}:
			default:
			}
		})
	}
	var ready atomic.Bool
	ready.Store(true)
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": buildVersion})
	})
	r.GET("/readyz", func(c *gin.Context) {
		if !ready.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "stopping"})
			return
		}
		sqlDB, err := db.DB()
		if err != nil || sqlDB.PingContext(c.Request.Context()) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "version": buildVersion})
	})
	r.GET("/metrics", handlers.MetricsHandler(db, wsHub))

	// 静态文件服务 - 上传文件
	// Uploaded content is private and is served only by authenticated handlers.
	// 静态文件服务 - 前端页面（从内嵌文件系统读取）
	webSub, _ := fs.Sub(webFS, "web/dist")
	if webSub != nil {
		fileServer := http.FileServer(http.FS(webSub))
		isStaticAsset := func(path string) bool {
			return strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") ||
				strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".ico") ||
				strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") ||
				strings.HasSuffix(path, ".jpeg") || strings.HasSuffix(path, ".svg") ||
				strings.HasSuffix(path, ".woff") || strings.HasSuffix(path, ".woff2")
		}
		r.Use(func(c *gin.Context) {
			path := c.Request.URL.Path
			if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/ws") || strings.HasPrefix(path, "/uploads") {
				c.Next()
				return
			}
			if isStaticAsset(path) {
				if strings.HasPrefix(path, "/assets/") {
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(c.Writer, c.Request)
			} else {
				c.Header("Cache-Control", "no-cache")
				data, _ := fs.ReadFile(webSub, "index.html")
				c.Data(200, "text/html; charset=utf-8", data)
			}
			c.Abort()
		})
	}

	// 初始化handlers
	authHandler := handlers.NewAuthHandler(db)
	dashboardHandler := handlers.NewDashboardHandler(db)
	deviceHandler := handlers.NewDeviceHandler(db)
	scriptHandler := handlers.NewScriptHandler(db)
	taskHandler := handlers.NewTaskHandler(db)
	resourceHandler := handlers.NewResourceHandler(db)
	templateHandler := handlers.NewTemplateHandler(db)
	dataHandler := handlers.NewDataHandler(db)
	systemHandler := handlers.NewSystemHandler(db)
	wsHandler := handlers.NewWebSocketHandler(db)
	clientUpdateHandler := handlers.NewClientUpdateHandler(db)

	// ============================================
	// API v1 路由
	// ============================================
	v1 := r.Group("/api/v1")

	// 认证路由 (无需JWT)
	auth := v1.Group("/auth")
	{
		auth.POST("/login", authHandler.Login)
		auth.POST("/logout", authHandler.Logout)
		auth.POST("/refresh", authHandler.RefreshToken)
	}

	// 公开路由 - 系统配置
	v1.GET("/system/config", authHandler.GetSystemConfig)
	v1.GET("/client-updates/latest", clientUpdateHandler.Latest)
	// 公开路由 - 截图文件（img标签无法带JWT token）
	v1.GET("/ws/screenshot-file/*filename", wsHandler.DownloadScreenshot)

	// WebSocket路由 (设备连接)
	r.GET("/ws/device/:device_id", handlers.DeviceWSHandler)

	// 需要JWT认证的路由
	api := v1.Group("")
	api.Use(middleware.JWTAuth(db))
	auditWriter := middleware.NewAuditLogWriter(db)
	api.Use(auditWriter.Middleware())
	{
		// 用户信息
		api.GET("/user/info", authHandler.GetUserInfo)
		api.POST("/user/change-password", authHandler.ChangePassword)
		api.PUT("/user/profile", authHandler.UpdateProfile)

		// 仪表盘
		api.GET("/dashboard/stats", dashboardHandler.GetStats)
		api.GET("/dashboard/realtime", dashboardHandler.GetRealtime)

		// WebSocket推送
		api.POST("/ws/push-task", wsHandler.PushTaskToDevice)
		api.POST("/ws/push-command", wsHandler.PushCommandToDevice)
		api.POST("/ws/push-command-batch", wsHandler.PushCommandToDevices)
		api.GET("/ws/online-devices", wsHandler.GetOnlineDevices)
		api.GET("/ws/devices-realtime", wsHandler.GetDevicesRealtime)
		api.GET("/ws/screenshots", wsHandler.ListScreenshots)
		api.GET("/ws/screen-frames", wsHandler.GetScreenFrames)
		api.GET("/devices/:id/metrics", wsHandler.GetDeviceMetrics)
		api.GET("/devices/:id/sms", wsHandler.GetDeviceSms)
		api.GET("/devices/:id/contacts", wsHandler.GetDeviceContacts)
		api.DELETE("/devices/:id/sms/:smsId", wsHandler.DeleteDeviceSms)
		api.DELETE("/devices/:id/sms", wsHandler.DeleteDeviceSms)
		api.DELETE("/devices/:id/contacts/:contactId", wsHandler.DeleteDeviceContacts)
		api.DELETE("/devices/:id/contacts", wsHandler.DeleteDeviceContacts)

		// 设备管理
		devices := api.Group("/devices")
		{
			devices.GET("", deviceHandler.ListDevices)
			devices.POST("", deviceHandler.CreateDevice)
			devices.PUT("/:id", deviceHandler.UpdateDevice)
			devices.POST("/:id/token", deviceHandler.RotateDeviceToken)
			devices.DELETE("/:id", deviceHandler.DeleteDevice)
			devices.DELETE("/batch", deviceHandler.BatchDeleteDevices)
			devices.POST("/batch/reset", deviceHandler.BatchResetDevices)
			devices.POST("/batch/add-group", deviceHandler.BatchAddDeviceGroup)
			devices.POST("/batch/builtin-task", deviceHandler.BatchBuiltinTask)
			devices.PUT("/:id/params", deviceHandler.UpdateDeviceParams)
			devices.GET("/:id/logs", deviceHandler.GetDeviceLogs)
		}

		// 设备分组
		deviceGroups := api.Group("/device-groups")
		{
			deviceGroups.GET("", deviceHandler.ListDeviceGroups)
			deviceGroups.POST("", deviceHandler.CreateDeviceGroup)
			deviceGroups.PUT("/:id", deviceHandler.UpdateDeviceGroup)
			deviceGroups.DELETE("/:id", deviceHandler.DeleteDeviceGroup)
			deviceGroups.POST("/:id/reset", deviceHandler.ResetGroupDevices)
			deviceGroups.GET("/:id/devices", deviceHandler.GetGroupDevices)
		}

		// 脚本管理
		scripts := api.Group("/scripts")
		{
			scripts.GET("", scriptHandler.ListScripts)
			scripts.POST("", scriptHandler.CreateScript)
			scripts.PUT("/:id", scriptHandler.UpdateScript)
			scripts.DELETE("/:id", scriptHandler.DeleteScript)
			scripts.GET("/:id/content", scriptHandler.GetScriptContent)
			scripts.POST("/:id/share", scriptHandler.ShareScript)
		}
		api.GET("/script-shares", scriptHandler.ListScriptShares)

		// 任务管理
		tasks := api.Group("/tasks")
		{
			tasks.GET("", taskHandler.ListTasks)
			tasks.POST("", taskHandler.CreateTask)
			tasks.PUT("/:id", taskHandler.UpdateTask)
			tasks.DELETE("/:id", taskHandler.DeleteTask)
			tasks.POST("/:id/start", taskHandler.StartTask)
			tasks.POST("/:id/stop", taskHandler.StopTask)
			tasks.POST("/:id/reset", taskHandler.ResetTask)
			tasks.POST("/batch/control", taskHandler.BatchControlTasks)
			tasks.POST("/:id/repair", taskHandler.RepairTask)
			tasks.GET("/:id/devices", taskHandler.GetTaskDevices)
			tasks.GET("/:id/logs", taskHandler.GetTaskLogs)
			tasks.POST("/:id/share", taskHandler.ShareTask)
			tasks.GET("/:id/merged-params", taskHandler.GetMergedTaskParams)
		}
		api.GET("/task-shares", taskHandler.ListTaskShares)

		clientUpdates := api.Group("/client-updates")
		{
			clientUpdates.GET("", clientUpdateHandler.List)
			clientUpdates.POST("", clientUpdateHandler.Create)
			clientUpdates.POST("/versions/:version/activate", clientUpdateHandler.Activate)
			clientUpdates.POST("/channels/:channel/rollback", clientUpdateHandler.Rollback)
		}

		// 资源管理
		resources := api.Group("/resources")
		{
			resources.GET("", resourceHandler.ListResources)
			resources.POST("/upload", resourceHandler.UploadResource)
			resources.DELETE("/:id", resourceHandler.DeleteResource)
			resources.PUT("/:id/replace", resourceHandler.ReplaceResource)
			resources.POST("/:id/share", resourceHandler.ShareResource)
			resources.GET("/:id/download", resourceHandler.DownloadResource)
		}
		api.GET("/resource-shares", resourceHandler.ListResourceShares)

		// 参数模板
		templates := api.Group("/templates")
		{
			templates.GET("", templateHandler.ListTemplates)
			templates.POST("", templateHandler.CreateTemplate)
			templates.PUT("/:id", templateHandler.UpdateTemplate)
			templates.DELETE("/:id", templateHandler.DeleteTemplate)
		}

		// 数据管理
		dataGroup := api.Group("/data")
		{
			dataGroup.GET("/templates", dataHandler.ListDataTemplates)
			dataGroup.POST("/templates", dataHandler.CreateDataTemplate)
			dataGroup.PUT("/templates/:id", dataHandler.UpdateDataTemplate)
			dataGroup.DELETE("/templates/:id", dataHandler.DeleteDataTemplate)
			dataGroup.GET("/records", dataHandler.ListDataRecords)
			dataGroup.POST("/records", dataHandler.CreateDataRecord)
			dataGroup.PUT("/records/:id", dataHandler.UpdateDataRecord)
			dataGroup.DELETE("/records/:id", dataHandler.DeleteDataRecord)
			dataGroup.GET("/permissions", dataHandler.ListDataPermissions)
			dataGroup.POST("/permissions", dataHandler.SetDataPermission)
			dataGroup.GET("/logs", dataHandler.ListDataLogs)
		}

		// 用户管理
		users := api.Group("/users")
		users.Use(middleware.RBAC(db, "system:user"))
		{
			users.GET("/admins", systemHandler.ListAdmins)
			users.GET("", systemHandler.ListUsers)
			users.POST("", systemHandler.CreateUser)
			users.PUT("/:id", systemHandler.UpdateUser)
			users.DELETE("/:id", systemHandler.DeleteUser)
			users.DELETE("/batch", systemHandler.BatchDeleteUsers)
			users.POST("/:id/roles", systemHandler.AssignUserRoles)
		}

		// 角色管理
		roles := api.Group("/roles")
		roles.Use(middleware.RBAC(db, "system:role"))
		{
			roles.GET("", systemHandler.ListRoles)
			roles.POST("", systemHandler.CreateRole)
			roles.PUT("/:id", systemHandler.UpdateRole)
			roles.DELETE("/:id", systemHandler.DeleteRole)
		}

		// 权限管理
		api.GET("/permissions", middleware.RBAC(db, "system:permission"), systemHandler.ListPermissions)

		// 系统日志
		api.GET("/system/logs", middleware.RBAC(db, "system:log"), systemHandler.ListSystemLogs)

		// 系统配置更新
		api.PUT("/system/config", middleware.RBAC(db, "system:config"), authHandler.UpdateSystemConfig)
	}

	// 启动服务
	addr := net.JoinHostPort(config.App.Server.BindAddress, strconv.Itoa(config.App.Server.Port))
	localURL := fmt.Sprintf("http://localhost:%d", config.App.Server.Port)
	log.Printf("Server starting on http://%s", addr)

	desktopMode := os.Getenv("CLOUD_DESKTOP_MODE") == "1"
	if !desktopMode {
		// Windows系统托盘图标 — 替代浏览器窗口
		if runtime.GOOS == "windows" {
			ip := getLocalIP()
			go runTray("云控 v72 | "+ip,
				func() {
					// 单击托盘 / 打开主页
					exec.Command("cmd", "/c", "start", localURL).Start()
				},
				func() {
					// 退出
					select {
					case desktopShutdown <- struct{}{}:
					default:
					}
				},
			)
		} else {
			// Linux/其他系统 — 打开浏览器
			go func() {
				time.Sleep(800 * time.Millisecond)
				exec.Command("xdg-open", localURL).Start()
			}()
		}
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	shutdownSignal := make(chan os.Signal, 1)
	shutdownDone := make(chan struct{})
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignal)
	go func() {
		select {
		case <-shutdownSignal:
		case <-desktopShutdown:
		}
		log.Printf("Shutdown signal received")
		ready.Store(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = httpServer.Shutdown(shutdownCtx)
		cancel()

		componentCtx, componentCancel := context.WithTimeout(context.Background(), 18*time.Second)
		scheduler.StopWithContext(componentCtx)
		if mqttBroker != nil {
			mqttBroker.Stop()
		}
		if externalMQTT != nil {
			externalMQTT.Stop()
		}
		wsHub.StopWithContext(componentCtx)
		auditWriter.Stop(componentCtx)
		componentCancel()
		if databaseDriver == "sqlite" {
			if result := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); result.Error != nil {
				log.Printf("SQLite final checkpoint failed: %v", result.Error)
			}
		}
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		close(shutdownDone)
	}()

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		// 端口被占用，尝试打开浏览器（可能已有实例在运行）
		if strings.Contains(err.Error(), "bind") || strings.Contains(err.Error(), "Only one usage") {
			log.Printf("Port %s already in use, opening browser to existing instance", addr)
			if runtime.GOOS == "windows" && !desktopMode {
				exec.Command("cmd", "/c", "start", localURL).Start()
			}
		} else {
			log.Fatalf("Failed to start server: %v", err)
		}
	} else if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
	}
}

func applyDatabaseEnvOverrides() {
	if value := os.Getenv("CLOUD_SERVER_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil && port > 0 {
			config.App.Server.Port = port
		}
	}
	if value := os.Getenv("CLOUD_MODE"); value != "" {
		config.App.Server.Mode = strings.TrimSpace(value)
	}
	if value := os.Getenv("CLOUD_DB_DRIVER"); value != "" {
		config.App.Database.Driver = value
	}
	if value := os.Getenv("CLOUD_DB_HOST"); value != "" {
		config.App.Database.Host = value
	}
	if value := os.Getenv("CLOUD_DB_USER"); value != "" {
		config.App.Database.User = value
	}
	if value := os.Getenv("CLOUD_DB_PASSWORD"); value != "" {
		config.App.Database.Password = value
	}
	if value := os.Getenv("CLOUD_DB_NAME"); value != "" {
		config.App.Database.DBName = value
	}
	if value := os.Getenv("CLOUD_DB_CHARSET"); value != "" {
		config.App.Database.Charset = value
	}
	if value := os.Getenv("CLOUD_DB_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil && port > 0 {
			config.App.Database.Port = port
		}
	}
}

func getLocalIP() string {
	// Use UDP dial to find preferred outbound IP
	conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
