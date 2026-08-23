package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword 使用bcrypt加密密码
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword 验证密码
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ValidatePassword keeps password policy consistent for bootstrap, user
// creation and password changes. bcrypt accepts at most 72 bytes, so checking
// here prevents a valid-looking request from failing only during hashing.
func ValidatePassword(password string) error {
	if password != strings.TrimSpace(password) {
		return errors.New("password must not start or end with whitespace")
	}
	if utf8.RuneCountInString(password) < 12 {
		return errors.New("password must contain at least 12 characters")
	}
	if len(password) > 72 {
		return errors.New("password must not exceed 72 bytes")
	}
	return nil
}

// GenerateRandomString 生成随机字符串
func GenerateRandomString(length int) string {
	bytes := make([]byte, length/2)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// GenerateDeviceID 生成设备唯一ID
func GenerateDeviceID() string {
	return fmt.Sprintf("DEV-%s-%s", time.Now().Format("20060102"), GenerateRandomString(6))
}

// EnsureDir 确保目录存在
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// SaveFile 保存上传文件
func SaveFile(src io.Reader, dstPath string) error {
	if err := EnsureDir(filepath.Dir(dstPath)); err != nil {
		return err
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// FileExists 检查文件是否存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
