package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeDocument struct {
	Services map[string]map[string]interface{} `yaml:"services"`
}

func TestDeploymentComposeFilesParse(t *testing.T) {
	base := loadComposeDocument(t, filepath.Join("..", "docker-compose.yml"))
	for _, service := range []string{"mysql", "mysql-backup", "server", "nginx", "prometheus", "emqx"} {
		if _, ok := base.Services[service]; !ok {
			t.Fatalf("base compose is missing service %q", service)
		}
	}

	tls := loadComposeDocument(t, filepath.Join("..", "docker-compose.tls.yml"))
	if _, ok := tls.Services["nginx"]; !ok {
		t.Fatal("TLS compose override is missing nginx")
	}

	external := loadComposeDocument(t, filepath.Join("..", "docker-compose.external-mqtt.yml"))
	if _, ok := external.Services["server"]; !ok {
		t.Fatal("external MQTT compose override is missing server")
	}

	edge := loadComposeDocument(t, filepath.Join("..", "docker-compose.edge.yml"))
	if len(edge.Services) != 2 || edge.Services["server"] == nil || edge.Services["nginx"] == nil {
		t.Fatal("edge compose must contain only server and nginx")
	}
	if edge.Services["server"]["mem_limit"] != "384m" || edge.Services["server"]["read_only"] != true {
		t.Fatal("edge server must keep its memory and read-only filesystem limits")
	}
	if edge.Services["nginx"]["mem_limit"] != "96m" || edge.Services["nginx"]["read_only"] != true {
		t.Fatal("edge nginx must keep its memory and read-only filesystem limits")
	}

	lowMySQL := loadComposeDocument(t, filepath.Join("..", "docker-compose.low-mysql.yml"))
	if lowMySQL.Services["mysql"] == nil || lowMySQL.Services["server"] == nil {
		t.Fatal("low-memory MySQL override is incomplete")
	}
}

func TestDockerBuildContextUsesAnAllowList(t *testing.T) {
	data, err := os.ReadFile(".dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "*\n") {
		t.Fatal("Docker context must start denied and explicitly allow required source files")
	}
	for _, required := range []string{"!go.mod", "!handlers/**", "!web/dist/**", "web/dist/dist/"} {
		if !strings.Contains(content, required) {
			t.Fatalf(".dockerignore is missing %q", required)
		}
	}
}

func loadComposeDocument(t *testing.T, path string) composeDocument {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document composeDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(document.Services) == 0 {
		t.Fatalf("%s has no services", path)
	}
	return document
}
