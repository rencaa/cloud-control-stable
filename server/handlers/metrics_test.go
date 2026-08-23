package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMetricsHandlerReportsHubState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := newTestHub(2, 1)

	// Use a production hub for initialized queues and replace only its clients.
	productionHub := NewWSHub(nil)
	productionHub.clients = hub.clients
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/metrics", nil)
	MetricsHandler(nil, productionHub)(context)

	body := recorder.Body.String()
	if !strings.Contains(body, "cloud_control_online_devices 2") ||
		!strings.Contains(body, "cloud_control_hub_queue_depth{queue=\"database\"} 0") {
		t.Fatalf("unexpected metrics output:\n%s", body)
	}
}
