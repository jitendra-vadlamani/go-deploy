package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go-deploy/internal/db"
)

func TestHandleBuildStreamsProgressAndDone(t *testing.T) {
	dbDir := t.TempDir()
	if err := db.Init(dbDir); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	sourceDir, err := filepath.Abs(filepath.Join("examples", "sample-app"))
	if err != nil {
		t.Fatalf("failed to resolve sample-app path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "go.mod")); err != nil {
		t.Skipf("sample-app not available: %v", err)
	}

	outputDir := t.TempDir()
	reqBody := map[string]any{
		"SourceDir":   sourceDir,
		"Name":        "sample-app-test",
		"OutputDir":   outputDir,
		"DefaultMode": "standalone",
		"Formats":     []string{"binary"},
		"Targets": []map[string]string{
			{"OS": runtime.GOOS, "Arch": runtime.GOARCH},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/build", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleBuild(rec, req)

	res := rec.Result()
	scanner := bufio.NewScanner(res.Body)
	var sawProgress, sawResult bool
	var doneEvent map[string]any

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var evt map[string]any
		if err := json.Unmarshal(line, &evt); err != nil {
			t.Fatalf("failed to parse NDJSON line %q: %v", line, err)
		}
		switch evt["type"] {
		case "progress":
			sawProgress = true
		case "result":
			sawResult = true
		case "done":
			doneEvent = evt
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("error reading response body: %v", err)
	}

	if !sawProgress {
		t.Error("expected at least one 'progress' event in the stream")
	}
	if !sawResult {
		t.Error("expected at least one 'result' event in the stream")
	}
	if doneEvent == nil {
		t.Fatal("expected a final 'done' event in the stream")
	}
	if success, _ := doneEvent["success"].(bool); !success {
		t.Errorf("expected build to succeed, got done event: %+v", doneEvent)
	}
}

func TestHandleBuildRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/build", nil)
	rec := httptest.NewRecorder()
	handleBuild(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHandleBuildRejectsNoTargets(t *testing.T) {
	dbDir := t.TempDir()
	if err := db.Init(dbDir); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	body, _ := json.Marshal(map[string]any{"SourceDir": ".", "Targets": []any{}})
	req := httptest.NewRequest(http.MethodPost, "/api/build", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handleBuild(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
}
