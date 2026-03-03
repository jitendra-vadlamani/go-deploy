# Go-Deploy Sample App

This is a simple Go web application designed to demonstrate the capabilities of **Go-Deploy**.

## Features
- Embedded static HTML page using `render:embed`.
- Simple API endpoint at `/api/hello`.
- Configurable port via `PORT` environment variable.

## Purpose
Use this project to test the **Go-Deploy Builder**. 

1.  **Build this binary first**:
    ```bash
    go build -o sample-app-bin main.go
    ```
2.  **Use Go-Deploy**:
    - Launch the Go-Deploy Builder.
    - Set the **Source Directory** to the absolute path of this folder.
    - Follow the wizard to package it as a system service.

## Running Locally (without Go-Deploy)
```bash
go run main.go
```
Visit `http://localhost:8080` to see the app in action.
