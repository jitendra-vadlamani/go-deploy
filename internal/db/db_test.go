package db

import (
	"os"
	"testing"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "go-deploy-db-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	if err := Init(dir); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	t.Cleanup(Close)
}

func TestSaveAndGetProject(t *testing.T) {
	setupTestDB(t)

	p := Project{
		ID:          "/path/to/project",
		Name:        "TestApp",
		Description: "A test app",
		Version:     "1.0.0",
		Formats:     []string{"binary", "zip"},
		BuildEnv:    map[string]string{"FOO": "bar"},
	}
	if err := SaveProject(p); err != nil {
		t.Fatalf("SaveProject failed: %v", err)
	}

	got, err := GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if got.Name != p.Name || got.Version != p.Version {
		t.Fatalf("GetProject returned %+v, want %+v", got, p)
	}
	if got.BuildEnv["FOO"] != "bar" {
		t.Fatalf("expected BuildEnv to round-trip, got %+v", got.BuildEnv)
	}
}

func TestGetProjectNotFound(t *testing.T) {
	setupTestDB(t)

	if _, err := GetProject("does-not-exist"); err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestDeleteProject(t *testing.T) {
	setupTestDB(t)

	p := Project{ID: "to-delete", Name: "Temp"}
	if err := SaveProject(p); err != nil {
		t.Fatalf("SaveProject failed: %v", err)
	}
	if err := DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
	if _, err := GetProject(p.ID); err == nil {
		t.Fatal("expected error after deleting project")
	}
}

func TestGetAllProjects(t *testing.T) {
	setupTestDB(t)

	want := []Project{
		{ID: "proj-a", Name: "A"},
		{ID: "proj-b", Name: "B"},
	}
	for _, p := range want {
		if err := SaveProject(p); err != nil {
			t.Fatalf("SaveProject failed: %v", err)
		}
	}

	got, err := GetAllProjects()
	if err != nil {
		t.Fatalf("GetAllProjects failed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("GetAllProjects returned %d projects, want %d", len(got), len(want))
	}
}
