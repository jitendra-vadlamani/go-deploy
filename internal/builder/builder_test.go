package builder

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRequiredToolsForPackaging(t *testing.T) {
	tests := []struct {
		name    string
		formats []string
		target  string
		want    []string
	}{
		{name: "no formats", formats: nil, target: "darwin", want: nil},
		{name: "darwin dmg", formats: []string{"dmg"}, target: "darwin", want: []string{"hdiutil"}},
		{name: "linux deb", formats: []string{"deb"}, target: "linux", want: []string{"dpkg-deb"}},
		{name: "windows exe", formats: []string{"exe"}, target: "windows", want: []string{"makensis"}},
		{name: "mixed", formats: []string{"deb", "dmg", "exe", "zip", "binary"}, target: "linux", want: []string{"dpkg-deb"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requiredToolsForPackaging(tt.formats, tt.target)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("requiredToolsForPackaging() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateSourceDir(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		err := validateSourceDir(filepath.Join(t.TempDir(), "missing"))
		if err == nil {
			t.Fatal("expected error for missing source dir")
		}
	})

	t.Run("missing go.mod", func(t *testing.T) {
		dir := t.TempDir()
		err := validateSourceDir(dir)
		if err == nil {
			t.Fatal("expected error for missing go.mod")
		}
	})

	t.Run("valid go module dir", func(t *testing.T) {
		dir := t.TempDir()
		goMod := []byte("module test\n\ngo 1.25.0\n")
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), goMod, 0644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		if err := validateSourceDir(dir); err != nil {
			t.Fatalf("expected valid source dir, got error: %v", err)
		}
	})
}
