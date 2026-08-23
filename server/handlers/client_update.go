package handlers

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ClientUpdateHandler struct{ DB *gorm.DB }

func NewClientUpdateHandler(db *gorm.DB) *ClientUpdateHandler { return &ClientUpdateHandler{DB: db} }

func releaseSignedMessage(release *models.ClientRelease) []byte {
	return []byte(release.Version + "\n" + release.Channel + "\n" + release.DownloadURL + "\n" + release.SHA256 + "\n")
}

func verifyClientRelease(release *models.ClientRelease) error {
	if config.App == nil || strings.TrimSpace(config.App.Updates.PublicKey) == "" {
		return errors.New("CLOUD_UPDATE_PUBLIC_KEY is not configured")
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(config.App.Updates.PublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid Ed25519 public key")
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(release.Signature))
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(publicKey), releaseSignedMessage(release), signature) {
		return errors.New("release signature verification failed")
	}
	return nil
}

func normalizeClientRelease(release *models.ClientRelease) error {
	release.Version = strings.TrimSpace(release.Version)
	release.Channel = strings.ToLower(strings.TrimSpace(release.Channel))
	if release.Channel == "" {
		release.Channel = "stable"
	}
	release.DownloadURL = strings.TrimSpace(release.DownloadURL)
	release.SHA256 = strings.ToLower(strings.TrimSpace(release.SHA256))
	release.Signature = strings.TrimSpace(release.Signature)
	if release.Version == "" || len(release.Version) > 32 || (release.Channel != "stable" && release.Channel != "beta") {
		return errors.New("invalid version or channel")
	}
	parsedURL, err := url.Parse(release.DownloadURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return errors.New("download_url must be HTTP or HTTPS")
	}
	digest, err := hex.DecodeString(release.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return errors.New("sha256 must be a 64-character digest")
	}
	if release.RolloutPercent < 1 || release.RolloutPercent > 100 {
		return errors.New("rollout_percent must be between 1 and 100")
	}
	release.Status = "draft"
	return verifyClientRelease(release)
}

func (h *ClientUpdateHandler) Create(c *gin.Context) {
	var release models.ClientRelease
	if err := c.ShouldBindJSON(&release); err != nil || normalizeClientRelease(&release) != nil {
		c.JSON(200, models.Response{Code: 400, Message: "升级清单无效或签名校验失败"})
		return
	}
	release.ID = 0
	if err := h.DB.Create(&release).Error; err != nil {
		c.JSON(200, models.Response{Code: 409, Message: "版本已存在或保存失败"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "已保存签名升级清单", Data: release})
}

func (h *ClientUpdateHandler) List(c *gin.Context) {
	var releases []models.ClientRelease
	h.DB.Order("id DESC").Limit(100).Find(&releases)
	c.JSON(200, models.Response{Code: 200, Message: "成功", Data: releases})
}

func (h *ClientUpdateHandler) Activate(c *gin.Context) {
	version := strings.TrimSpace(c.Param("version"))
	channel := strings.ToLower(strings.TrimSpace(c.DefaultQuery("channel", "stable")))
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var release models.ClientRelease
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("version = ? AND channel = ?", version, channel).First(&release).Error; err != nil {
			return err
		}
		if err := verifyClientRelease(&release); err != nil {
			return err
		}
		var previous models.ClientRelease
		if err := tx.Where("channel = ? AND status = ?", release.Channel, "active").First(&previous).Error; err == nil && previous.ID != release.ID {
			release.PreviousVersion = previous.Version
			if err := tx.Model(&previous).Update("status", "retired").Error; err != nil {
				return err
			}
		}
		return tx.Model(&release).Updates(map[string]interface{}{"status": "active", "previous_version": release.PreviousVersion}).Error
	})
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "启用失败：版本不存在或签名无效"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "灰度版本已启用"})
}

func (h *ClientUpdateHandler) Rollback(c *gin.Context) {
	channel := strings.ToLower(strings.TrimSpace(c.Param("channel")))
	if channel != "stable" && channel != "beta" {
		c.JSON(200, models.Response{Code: 400, Message: "升级通道无效"})
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var current models.ClientRelease
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("channel = ? AND status = ?", channel, "active").First(&current).Error; err != nil {
			return err
		}
		if current.PreviousVersion == "" {
			return errors.New("no previous version")
		}
		var previous models.ClientRelease
		if err := tx.Where("channel = ? AND version = ?", channel, current.PreviousVersion).First(&previous).Error; err != nil {
			return err
		}
		if err := verifyClientRelease(&previous); err != nil {
			return err
		}
		if err := tx.Model(&current).Update("status", "rolled_back").Error; err != nil {
			return err
		}
		return tx.Model(&previous).Updates(map[string]interface{}{"status": "active", "previous_version": ""}).Error
	})
	if err != nil {
		c.JSON(200, models.Response{Code: 400, Message: "没有可回滚的已签名版本"})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "已回滚到上一版本"})
}

func (h *ClientUpdateHandler) Latest(c *gin.Context) {
	channel := strings.ToLower(strings.TrimSpace(c.DefaultQuery("channel", "stable")))
	if channel != "stable" && channel != "beta" {
		channel = "stable"
	}
	var release models.ClientRelease
	if err := h.DB.Where("channel = ? AND status = ?", channel, "active").First(&release).Error; err != nil || verifyClientRelease(&release) != nil {
		c.JSON(200, models.Response{Code: 200, Message: "暂无升级", Data: gin.H{"available": false}})
		return
	}
	deviceID := strings.TrimSpace(c.Query("device_id"))
	currentVersion := strings.TrimSpace(c.Query("version"))
	if currentVersion == release.Version || !deviceIncludedInRollout(deviceID, release.Version, release.RolloutPercent) {
		c.JSON(200, models.Response{Code: 200, Message: "暂无升级", Data: gin.H{"available": false}})
		return
	}
	c.JSON(200, models.Response{Code: 200, Message: "发现新版本", Data: gin.H{
		"available": true, "version": release.Version, "channel": release.Channel,
		"download_url": release.DownloadURL, "sha256": release.SHA256, "signature": release.Signature,
		"notes": release.Notes, "previous_version": release.PreviousVersion, "requires_confirmation": true,
	}})
}

func deviceIncludedInRollout(deviceID, version string, rollout int) bool {
	if rollout >= 100 {
		return true
	}
	sum := sha256.Sum256([]byte(deviceID + "\x00" + version))
	bucket := int(binary.BigEndian.Uint32(sum[:4]) % 100)
	return bucket < rollout
}
