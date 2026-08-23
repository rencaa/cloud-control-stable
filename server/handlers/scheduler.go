package handlers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// TaskScheduler 定时任务调度器
type TaskScheduler struct {
	cron            *cron.Cron
	db              *gorm.DB
	mu              sync.Mutex
	jobs            map[uint64]cron.EntryID
	stop            chan struct{}
	stopOnce        sync.Once
	dispatchQueue   chan uint64
	dispatchOnce    sync.Once
	dispatchWG      sync.WaitGroup
	dispatchPending sync.Map
	stopping        atomic.Bool
	backgroundOnce  sync.Once
	backgroundWG    sync.WaitGroup
}

var Scheduler *TaskScheduler

// NewTaskScheduler 创建调度器
func NewTaskScheduler(db *gorm.DB) *TaskScheduler {
	s := &TaskScheduler{
		cron:          cron.New(cron.WithSeconds(), cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger))),
		db:            db,
		jobs:          make(map[uint64]cron.EntryID),
		stop:          make(chan struct{}),
		dispatchQueue: make(chan uint64, 256),
	}
	Scheduler = s
	return s
}

// Start 启动调度器，加载所有启用的定时任务
func (s *TaskScheduler) Start() {
	if s == nil || s.stopping.Load() {
		return
	}
	s.startDispatchWorkers()
	var tasks []models.Task
	s.db.Where("cron_enabled = ? AND cron_expr != ?", true, "").Find(&tasks)
	now := time.Now()
	for _, t := range tasks {
		missedAt := t.NextRunAt
		if err := s.addTaskConfig(t.ID, t.CronExpr, t.CronTimezone); err != nil {
			continue
		}
		policy, _ := normalizeMisfirePolicy(t.MisfirePolicy)
		if missedAt != nil && !missedAt.After(now) && policy != "skip" && now.Sub(*missedAt) <= cronCatchupWindow() {
			log.Printf("Scheduler: catch-up task %d missed at %s", t.ID, missedAt.Format(time.RFC3339))
			s.executeTask(t.ID)
		} else if missedAt != nil && !missedAt.After(now) {
			log.Printf("Scheduler: skipped stale catch-up task %d missed at %s", t.ID, missedAt.Format(time.RFC3339))
		}
	}
	s.cron.Start()
	log.Printf("TaskScheduler started with %d jobs", len(s.jobs))
	// 启动离线监控
	go s.monitorOffline()
}

// monitorOffline 检测离线设备并更新状态
func (s *TaskScheduler) monitorOffline() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			fiveMinAgo := time.Now().Add(-5 * time.Minute)
			// 将超过5分钟没心跳的设备标记为离线
			s.db.Model(&models.Device{}).
				Where("status > 0 AND last_heartbeat IS NOT NULL AND last_heartbeat < ?", fiveMinAgo).
				Update("status", 0)
		}
	}
}

// Stop 停止调度器
func (s *TaskScheduler) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s.StopWithContext(ctx)
}

func (s *TaskScheduler) StopWithContext(ctx context.Context) {
	if s == nil {
		return
	}
	s.startDispatchWorkers()
	s.stopping.Store(true)
	cronDone := s.cron.Stop()
	s.stopOnce.Do(func() { close(s.stop) })
	done := make(chan struct{})
	go func() {
		<-cronDone.Done()
		s.dispatchWG.Wait()
		s.backgroundWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("Scheduler shutdown timed out with %d dispatch jobs queued", len(s.dispatchQueue))
	}
}

func (s *TaskScheduler) startDispatchWorkers() {
	s.dispatchOnce.Do(func() {
		const workers = 1
		s.dispatchWG.Add(workers)
		for i := 0; i < workers; i++ {
			go func() { defer s.dispatchWG.Done(); s.runDispatchWorker() }()
		}
	})
}

func (s *TaskScheduler) EnqueueTask(taskID uint64) bool {
	if s == nil || taskID == 0 || s.stopping.Load() {
		return false
	}
	s.startDispatchWorkers()
	if _, loaded := s.dispatchPending.LoadOrStore(taskID, struct{}{}); loaded {
		return true
	}
	select {
	case s.dispatchQueue <- taskID:
		return true
	default:
		s.dispatchPending.Delete(taskID)
		return false
	}
}

func (s *TaskScheduler) runDispatchWorker() {
	for {
		select {
		case taskID := <-s.dispatchQueue:
			s.executeQueuedDispatch(taskID)
		case <-s.stop:
			for {
				select {
				case taskID := <-s.dispatchQueue:
					s.executeQueuedDispatch(taskID)
				default:
					return
				}
			}
		}
	}
}

func (s *TaskScheduler) executeQueuedDispatch(taskID uint64) {
	defer s.dispatchPending.Delete(taskID)
	var task models.Task
	if err := s.db.Preload("Script").First(&task, taskID).Error; err != nil || task.Status != 1 {
		return
	}
	result := dispatchTaskToDevices(s.db, &task)
	message := fmt.Sprintf("dispatch complete: sent=%d queued=%d offline=%d busy=%d", result.Sent, result.Queued, result.Offline, result.Busy)
	_ = s.db.Create(&models.TaskLog{TaskID: task.ID, LogType: "info", Message: message}).Error
	if result.Sent == 0 {
		_ = s.db.Model(&task).Update("status", 0).Error
	}
}

// AddTask 添加/更新定时任务
func (s *TaskScheduler) AddTask(taskID uint64, cronExpr string) error {
	var task models.Task
	if err := s.db.Select("cron_timezone").First(&task, taskID).Error; err != nil {
		return err
	}
	return s.addTaskConfig(taskID, cronExpr, task.CronTimezone)
}

func (s *TaskScheduler) addTaskConfig(taskID uint64, cronExpr, timezone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldID, ok := s.jobs[taskID]; ok {
		s.cron.Remove(oldID)
	}

	if cronExpr == "" {
		delete(s.jobs, taskID)
		return nil
	}

	scheduledExpr, err := cronExpressionForTimezone(cronExpr, timezone)
	if err != nil {
		return err
	}
	id, err := s.cron.AddFunc(scheduledExpr, func() {
		s.executeTask(taskID)
		s.persistNextRun(taskID, cronExpr, timezone, time.Now())
	})
	if err != nil {
		log.Printf("Scheduler: bad cron for task %d: %v", taskID, err)
		return err
	}
	s.jobs[taskID] = id
	s.persistNextRun(taskID, cronExpr, timezone, time.Now())
	log.Printf("Scheduler: task %d scheduled: %s timezone=%s", taskID, cronExpr, timezone)
	return nil
}

func ValidateCronExpression(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return nil
	}
	_, err := cronSchedule(expr)
	return err
}

func cronExpressionForTimezone(expr, timezone string) (string, error) {
	if strings.TrimSpace(expr) == "" {
		return "", nil
	}
	timezone, err := normalizeCronTimezone(timezone)
	if err != nil {
		return "", err
	}
	return "CRON_TZ=" + timezone + " " + strings.TrimSpace(expr), nil
}

func cronSchedule(expr string) (cron.Schedule, error) {
	return cronScheduleInTimezone(expr, defaultCronTimezone)
}

func cronScheduleInTimezone(expr, timezone string) (cron.Schedule, error) {
	expr, err := cronExpressionForTimezone(expr, timezone)
	if err != nil {
		return nil, err
	}
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return parser.Parse(expr)
}

func cronCatchupWindow() time.Duration {
	if config.App == nil || config.App.Reliability.CronCatchupHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(config.App.Reliability.CronCatchupHours) * time.Hour
}

func (s *TaskScheduler) persistNextRun(taskID uint64, expr, timezone string, after time.Time) {
	schedule, err := cronScheduleInTimezone(expr, timezone)
	if err != nil {
		return
	}
	next := schedule.Next(after)
	if err := s.db.Model(&models.Task{}).Where("id = ?", taskID).Update("next_run_at", next).Error; err != nil {
		log.Printf("Scheduler: persist next run failed task=%d: %v", taskID, err)
	}
}

// RemoveTask 移除定时任务
func (s *TaskScheduler) RemoveTask(taskID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.jobs[taskID]; ok {
		s.cron.Remove(id)
		delete(s.jobs, taskID)
	}
	_ = s.db.Model(&models.Task{}).Where("id = ?", taskID).Update("next_run_at", nil).Error
}

// executeTask 执行定时任务
func (s *TaskScheduler) executeTask(taskID uint64) {
	var task models.Task
	if err := s.db.Preload("Script").First(&task, taskID).Error; err != nil {
		return
	}
	if !task.CronEnabled {
		return
	}
	// A cron tick never overlaps a still-running occurrence. SkipIfStillRunning
	// only protects the short callback; this DB state protects the async worker.
	if task.Status == 1 {
		log.Printf("Scheduler: task %d is still running; tick coalesced", taskID)
		return
	}
	now := time.Now()
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&task).Updates(map[string]interface{}{"last_run_at": now, "status": 1}).Error; err != nil {
			return err
		}
		// Every occurrence gets a fresh per-device state. Otherwise a success
		// from yesterday can make today's partially-offline run look complete.
		return tx.Model(&models.TaskDevice{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
			"status": 0, "result": "", "started_at": nil, "deadline_at": nil, "finished_at": nil,
		}).Error
	}); err != nil {
		return
	}

	if !s.EnqueueTask(taskID) {
		_ = s.db.Model(&task).Update("status", 0).Error
		log.Printf("Scheduler: dispatch queue full for task %d (%s)", taskID, task.Name)
	}
}
