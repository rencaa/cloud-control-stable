package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLimitAPIRequestBodyRejectsOversizedContentLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LimitAPIRequestBody(8))
	router.POST("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("123456789"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLimitAPIRequestBodyBoundsChunkedPayloadAndSkipsMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LimitAPIRequestBody(8))
	router.POST("/read", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	})
	router.POST("/upload", func(c *gin.Context) { c.Status(http.StatusOK) })

	chunked := httptest.NewRequest(http.MethodPost, "/read", strings.NewReader("123456789"))
	chunked.ContentLength = -1
	chunked.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, chunked)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked status=%d", response.Code)
	}

	multipart := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("123456789"))
	multipart.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, multipart)
	if response.Code != http.StatusOK {
		t.Fatalf("multipart status=%d", response.Code)
	}
}
