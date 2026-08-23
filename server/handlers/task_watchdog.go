package handlers

import (
	"fmt"
	"log"
	"time"

	"cloud-control-server/models"

	"gorm.io/gorm"
)

type expiredTaskRun struct {
	ID       uint64
	TaskID   uint64
	DeviceID string
	RunID    string
}

func (s *TaskScheduler) watchTaskDeadlines() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	s.expireTaskRuns(time.Now())
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.expireTaskRuns(now)
		}
	}
}

func (s *TaskScheduler) expireTaskRuns(now time.Time) {
	var expired []expiredTaskRun
	err := s.db.Table("task_devices AS td").
		Select("td.id, td.task_id, d.device_id, td.run_id").
		Joins("JOIN devices AS d ON d.id = td.device_id").
		Where("td.status = ? AND td.deadline_at IS NOT NULL AND td.deadline_at <= ?", 1, now).
		Order("td.deadline_at ASC").Limit(100).Scan(&expired).Error
	if err != nil {
		log.Printf("Task watchdog query failed: %v", err)
		return
	}
	for _, run := range expired {
		changed := false
		err := s.db.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&models.TaskDevice{}).Where("id = ? AND status = ? AND run_id = ?", run.ID, 1, run.RunID).
				Updates(map[string]interface{}{
					"status": 4, "result": "任务执行超时，已由服务端看门狗终止", "finished_at": now, "deadline_at": nil,
				})
			if result.Error != nil || result.RowsAffected == 0 {
				return result.Error
			}
			changed = true
			if err := tx.Create(&models.TaskLog{TaskID: run.TaskID, LogType: "error", Message: fmt.Sprintf("run %s timed out", run.RunID)}).Error; err != nil {
				return err
			}
			var pending int64
			if err := tx.Model(&models.TaskDevice{}).Where("task_id = ? AND status IN ?", run.TaskID, []int{0, 1}).Count(&pending).Error; err != nil {
				return err
			}
			if pending == 0 {
				return tx.Model(&models.Task{}).Where("id = ? AND status = ?", run.TaskID, 1).Update("status", 2).Error
			}
			return nil
		})
		if err != nil || !changed {
			continue
		}
		if Hub != nil {
			_ = Hub.SendCommandWithDelivery(run.DeviceID, WSMessage{Type: "command", Data: map[string]interface{}{
				"cmd_id": newCommandID("timeout-task"), "command": "cancel_task", "task_id": run.TaskID,
				"run_id": run.RunID, "reason": "timeout",
			}}, run.TaskID)
		}
	}
}
