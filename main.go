package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"go-deploy/internal/builder"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"

	"strings"

	"github.com/sqweek/dialog"
	webview "github.com/webview/webview_go"
)

//go:embed frontend/index.html
var frontendAssets embed.FS

func main() {
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
	w.SetSize(600, 800, webview.HintNone)

	// Bind folder selection
	w.Bind("selectFolder", func() string {
		directory, err := dialog.Directory().Title("Select Source Directory").Browse()
		if err != nil {
			return ""
		}
		return directory
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

	var buildErrors []string
	for _, t := range req.Targets {
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
			buildErrors = append(buildErrors, fmt.Sprintf("%s/%s: %v", t.OS, t.Arch, err))
		}
	}

	if len(buildErrors) > 0 {
		http.Error(w, fmt.Sprintf("Some builds failed:\n%s", strings.Join(buildErrors, "\n")), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Successfully built appliances in %s", outputDir)
}
