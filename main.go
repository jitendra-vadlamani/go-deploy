package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"go-deploy/internal/builder"
	"go-deploy/internal/db"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sqweek/dialog"
	webview "github.com/webview/webview_go"
)

//go:embed frontend/index.html
var frontendAssets embed.FS

func main() {
	// 0. Initialize DB
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".go-deploy", "data")
	os.MkdirAll(dbPath, 0755)
	if err := db.Init(dbPath); err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 1. Find a free port for the internal API/Static server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// 2. Start internal background server
	mux := http.NewServeMux()

	// Serve embedded frontend
	subFS, _ := fs.Sub(frontendAssets, "frontend")
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	// API for building
	mux.HandleFunc("/api/build", handleBuild)

	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux))
	}()

	// 3. Create and launch WebView
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Go-Deploy")
	w.SetSize(800, 900, webview.HintNone)

	// Bindings
	w.Bind("selectFolder", func() string {
		directory, err := dialog.Directory().Title("Select Directory").Browse()
		if err != nil {
			return ""
		}
		return directory
	})

	w.Bind("selectFile", func() string {
		file, err := dialog.File().Title("Select File").Load()
		if err != nil {
			return ""
		}
		return file
	})

	w.Bind("getProjectInfo", func(sourceDir string) map[string]string {
		info := make(map[string]string)
		modPath := filepath.Join(sourceDir, "go.mod")
		if _, err := os.Stat(modPath); err != nil {
			info["name"] = filepath.Base(sourceDir)
			info["version"] = "1.0.0"
			return info
		}
		f, err := os.Open(modPath)
		if err == nil {
			defer f.Close()
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "module ") {
					modName := strings.TrimPrefix(line, "module ")
					parts := strings.Split(modName, "/")
					info["name"] = parts[len(parts)-1]
					break
				}
			}
		}
		if info["name"] == "" {
			info["name"] = filepath.Base(sourceDir)
		}
		info["version"] = "1.0.0" // Default version
		return info
	})

	w.Bind("saveProject", func(configJson string) bool {
		var p db.Project
		if err := json.Unmarshal([]byte(configJson), &p); err != nil {
			return false
		}
		if p.ID == "" {
			return false
		}
		p.UpdatedAt = time.Now().Unix()
		if err := db.SaveProject(p); err != nil {
			return false
		}
		return true
	})

	w.Bind("getRecentProjects", func() []db.Project {
		projects, _ := db.GetAllProjects()
		return projects
	})

	w.Bind("deleteProject", func(id string) bool {
		return db.DeleteProject(id) == nil
	})

	w.Bind("openFolder", func(path string) {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("explorer", path)
		case "darwin":
			cmd = exec.Command("open", path)
		default: // linux
			cmd = exec.Command("xdg-open", path)
		}
		cmd.Run()
	})

	w.Bind("loadEnv", func(path string) map[string]string {
		env := make(map[string]string)
		f, err := os.Open(path)
		if err != nil {
			return env
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				env[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
		return env
	})

	w.Navigate(fmt.Sprintf("http://127.0.0.1:%d", port))
	w.Run()
}

func handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type Target struct {
		OS   string `json:"OS"`
		Arch string `json:"Arch"`
	}
	var req struct {
		SourceDir   string            `json:"SourceDir"`
		BuildEnv    map[string]string `json:"BuildEnv"`
		Name        string            `json:"Name"`
		Description string            `json:"Description"`
		Version     string            `json:"Version"`
		BrowserURL  string            `json:"BrowserURL"`
		OutputDir   string            `json:"OutputDir"`
		Targets     []Target          `json:"Targets"`
		DefaultMode string            `json:"DefaultMode"`
		Formats     []string          `json:"Formats"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Targets) == 0 {
		http.Error(w, "No build targets selected", http.StatusBadRequest)
		return
	}

	// Default output dir
	outputDir := req.OutputDir
	if outputDir == "" {
		outputDir = "./builds"
	}
	os.MkdirAll(outputDir, 0755)

	type BuildResult struct {
		Target string `json:"target"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	var results []BuildResult
	anySuccess := false

	for _, t := range req.Targets {
		targetName := fmt.Sprintf("%s/%s", t.OS, t.Arch)
		opts := builder.BuildOptions{
			SourceDir:   req.SourceDir,
			Name:        req.Name,
			Description: req.Description,
			Version:     req.Version,
			BrowserURL:  req.BrowserURL,
			OutputDir:   outputDir,
			TargetOS:    t.OS,
			TargetArch:  t.Arch,
			DefaultMode: req.DefaultMode,
			BuildEnv:    req.BuildEnv,
			Formats:     req.Formats,
		}

		err = builder.Build(opts)
		if err != nil {
			results = append(results, BuildResult{
				Target: targetName,
				Status: "failed",
				Error:  err.Error(),
			})
		} else {
			results = append(results, BuildResult{
				Target: targetName,
				Status: "success",
			})
			anySuccess = true
		}
	}

	// Auto-save project on build attempt
	p := db.Project{
		ID:          req.SourceDir,
		Name:        req.Name,
		Description: req.Description,
		Version:     req.Version,
		BrowserURL:  req.BrowserURL,
		OutputDir:   req.OutputDir,
		DefaultMode: req.DefaultMode,
		BuildEnv:    req.BuildEnv,
		Formats:     req.Formats,
		UpdatedAt:   time.Now().Unix(),
	}
	db.SaveProject(p)

	w.Header().Set("Content-Type", "application/json")

	response := struct {
		Success   bool          `json:"success"`
		Message   string        `json:"message"`
		Results   []BuildResult `json:"results"`
		OutputDir string        `json:"outputDir"`
	}{
		Success:   anySuccess,
		Results:   results,
		OutputDir: outputDir,
	}

	if anySuccess {
		absOutput, _ := filepath.Abs(outputDir)
		response.Message = fmt.Sprintf("Built appliances in %s", absOutput)
		json.NewEncoder(w).Encode(response)
	} else {
		response.Message = "All builds failed"
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
	}
}
