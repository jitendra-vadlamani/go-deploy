package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
)

//go:embed static/*
var staticAssets embed.FS

func main() {
	// Root filesystem for static assets
	subFS, err := fs.Sub(staticAssets, "static")
	if err != nil {
		log.Fatal(err)
	}

	// Serve static files
	http.Handle("/", http.FileServer(http.FS(subFS)))

	// API simple endpoint
	http.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from the Go-Deploy Sample API!")
	})

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Sample app starting on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
