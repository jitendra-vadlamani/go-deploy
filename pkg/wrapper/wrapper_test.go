package wrapper

import "testing"

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "MyApp", want: "MyApp"},
		{name: "spaces and symbols", in: "My App! v2", want: "My-App-v2"},
		{name: "empty", in: "   ", want: "app"},
		{name: "leading/trailing separators", in: "--App--", want: "App"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFileName(tt.in); got != tt.want {
				t.Fatalf("sanitizeFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultCommandHint(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "appliance temp binary", in: "/tmp/appliance_bin_12345", want: "appliance_bin_"},
		{name: "named binary", in: "/usr/local/bin/MyApp", want: "myapp"},
		{name: "empty path", in: "", want: "appliance_bin_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultCommandHint(tt.in); got != tt.want {
				t.Fatalf("defaultCommandHint(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEffectiveBrowserURL(t *testing.T) {
	if got := effectiveBrowserURL(""); got != defaultBrowserURL {
		t.Fatalf("effectiveBrowserURL(\"\") = %q, want %q", got, defaultBrowserURL)
	}
	if got := effectiveBrowserURL("  "); got != defaultBrowserURL {
		t.Fatalf("effectiveBrowserURL(whitespace) = %q, want %q", got, defaultBrowserURL)
	}
	custom := "http://localhost:9090"
	if got := effectiveBrowserURL(custom); got != custom {
		t.Fatalf("effectiveBrowserURL(%q) = %q, want %q", custom, got, custom)
	}
}

func TestLoadStandaloneStateMissingFile(t *testing.T) {
	state, path, err := loadStandaloneState("nonexistent-app-" + t.Name())
	if err != nil {
		t.Fatalf("expected no error for missing state file, got %v", err)
	}
	if state != nil {
		t.Fatalf("expected nil state for missing file, got %+v", state)
	}
	if path == "" {
		t.Fatal("expected a non-empty path even when file is missing")
	}
}

func TestIsProcessAlive(t *testing.T) {
	// -1 and 0 are special-cased by POSIX kill(2) (broadcast / process-group
	// signals), so use a PID far outside any real range instead.
	const unlikelyPID = 999999999
	if isProcessAlive(unlikelyPID) {
		t.Fatalf("expected PID %d to be reported as not alive", unlikelyPID)
	}
}
