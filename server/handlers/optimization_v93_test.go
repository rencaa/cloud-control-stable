package handlers

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestTaskPolicyValidation(t *testing.T) {
	if got := normalizeTaskTimeout(0); got != 3600 {
		t.Fatalf("default timeout = %d", got)
	}
	if validateTaskTimeout(29) == nil || validateTaskTimeout(86401) == nil {
		t.Fatal("timeout bounds were not enforced")
	}
	if timezone, err := normalizeCronTimezone(""); err != nil || timezone != "Asia/Shanghai" {
		t.Fatalf("default timezone = %q err=%v", timezone, err)
	}
	if _, err := normalizeCronTimezone("No/Such_Zone"); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	for _, policy := range []string{"skip", "run_once", "latest"} {
		if _, err := normalizeMisfirePolicy(policy); err != nil {
			t.Fatalf("policy %s rejected: %v", policy, err)
		}
	}
	if _, err := cronScheduleInTimezone("0 0 9 * * *", "Asia/Shanghai"); err != nil {
		t.Fatalf("timezone cron rejected: %v", err)
	}
}

func TestTaskWatchdogExpiresRunningDevice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.TaskDevice{}, &models.Device{}, &models.TaskLog{}); err != nil {
		t.Fatal(err)
	}
	task := models.Task{Name: "timeout", ScriptID: 1, Status: 1, TimeoutSeconds: 30}
	device := models.Device{DeviceID: "watchdog-device", Name: "watchdog"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(-time.Second)
	run := models.TaskDevice{TaskID: task.ID, DeviceID: device.ID, Status: 1, RunID: "run-timeout", DeadlineAt: &deadline}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	oldHub := Hub
	Hub = nil
	defer func() { Hub = oldHub }()
	scheduler := NewTaskScheduler(db)
	scheduler.expireTaskRuns(time.Now())
	var got models.TaskDevice
	if err := db.First(&got, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != 4 || got.FinishedAt == nil || got.DeadlineAt != nil {
		t.Fatalf("watchdog state = %+v", got)
	}
}

func TestSignedClientReleaseAndStableRollout(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldConfig := config.App
	config.App = &config.Config{Updates: config.UpdateConfig{PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}
	defer func() { config.App = oldConfig }()
	release := models.ClientRelease{
		Version: "9.3.0", Channel: "stable", DownloadURL: "http://192.168.1.2/agent.apk",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RolloutPercent: 10,
	}
	release.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, releaseSignedMessage(&release)))
	if err := normalizeClientRelease(&release); err != nil {
		t.Fatalf("signed release rejected: %v", err)
	}
	first := deviceIncludedInRollout("device-1", release.Version, release.RolloutPercent)
	for i := 0; i < 20; i++ {
		if deviceIncludedInRollout("device-1", release.Version, release.RolloutPercent) != first {
			t.Fatal("rollout assignment is not stable")
		}
	}
	release.DownloadURL += "?tampered=1"
	if err := verifyClientRelease(&release); err == nil {
		t.Fatal("tampered release signature accepted")
	}
}
