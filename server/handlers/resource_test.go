package handlers

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func multipartResourceRequest(t *testing.T, payload []byte, filename string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestWriteUploadedResourcePublishesCompleteFile(t *testing.T) {
	request := multipartResourceRequest(t, []byte("new resource content"), "document.txt")
	if err := request.ParseMultipartForm(1024 * 1024); err != nil {
		t.Fatal(err)
	}
	header := request.MultipartForm.File["file"][0]
	destination := filepath.Join(t.TempDir(), "nested", "document.txt")
	if err := writeUploadedResource(header, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new resource content" {
		t.Fatalf("saved content = %q", data)
	}
	if _, err := os.Stat(destination + ".uploading"); !os.IsNotExist(err) {
		t.Fatalf("temporary file was left behind: %v", err)
	}
}

func TestReplaceResourcePublishesNewPathBeforeRemovingOldFile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Role{}, &models.UserRole{}, &models.Resource{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Username: "resource-owner", Password: "hashed"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	uploadRoot := t.TempDir()
	oldPath := filepath.Join(uploadRoot, "old.txt")
	if err := os.WriteFile(oldPath, []byte("old content"), 0600); err != nil {
		t.Fatal(err)
	}
	resource := models.Resource{Name: "resource", Filename: "old.txt", FilePath: oldPath, FileSize: 11, UserID: owner.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	previousConfig := config.App
	config.App = &config.Config{Upload: config.UploadConfig{MaxSize: 1024 * 1024, UploadPath: uploadRoot}}
	t.Cleanup(func() { config.App = previousConfig })

	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(writer)
	context.Request = multipartResourceRequest(t, []byte("new content"), "new.txt")
	context.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(resource.ID, 10)}}
	context.Set("user_id", owner.ID)
	NewResourceHandler(db).ReplaceResource(context)
	if writer.Code != http.StatusOK {
		t.Fatalf("replace status = %d, body=%s", writer.Code, writer.Body.String())
	}
	if err := db.First(&resource, resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resource.FilePath == oldPath || resource.Filename != "new.txt" || resource.FileSize != int64(len("new content")) {
		t.Fatalf("resource metadata was not switched: %+v", resource)
	}
	file, err := os.Open(resource.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(data) != "new content" {
		t.Fatalf("new resource content invalid: read=%v close=%v content=%q", readErr, closeErr, data)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old resource remained after successful replacement: %v", err)
	}
}

func TestDeleteResourceRestoresFileWhenDatabaseDeleteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Role{}, &models.UserRole{}, &models.Resource{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Username: "delete-owner", Password: "hashed"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "resource.txt")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	resource := models.Resource{Name: "resource", Filename: "resource.txt", FilePath: path, UserID: owner.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	db.Callback().Delete().Before("gorm:delete").Register("test:fail_resource_delete", func(tx *gorm.DB) {
		if tx.Statement.Table == "resources" {
			tx.AddError(io.ErrUnexpectedEOF)
		}
	})

	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(writer)
	context.Request = httptest.NewRequest(http.MethodDelete, "/", nil)
	context.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(resource.ID, 10)}}
	context.Set("user_id", owner.ID)
	NewResourceHandler(db).DeleteResource(context)
	if writer.Code != http.StatusOK || !bytes.Contains(writer.Body.Bytes(), []byte("资源记录删除失败")) {
		t.Fatalf("unexpected delete failure response: status=%d body=%s", writer.Code, writer.Body.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("resource file was not restored after database failure: err=%v data=%q", err, data)
	}
}

func TestResourceStorageQuotaAccountsForReplacement(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Resource{Name: "used", Filename: "used.bin", FileSize: 80}).Error; err != nil {
		t.Fatal(err)
	}
	previousConfig := config.App
	config.App = &config.Config{Upload: config.UploadConfig{MaxSize: 50, MaxTotalBytes: 100}}
	t.Cleanup(func() { config.App = previousConfig })

	if resourceStorageAvailable(db, 30, 0) {
		t.Fatal("new upload exceeded total quota")
	}
	if !resourceStorageAvailable(db, 30, 20) {
		t.Fatal("replacement should reuse the old resource allocation")
	}
}
