package builder

import (
	"archive/zip"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

type BuildOptions struct {
	SourceDir   string            // Path to Go source directory
	BuildEnv    map[string]string // Environment variables for go build
	Name        string
	Description string
	Version     string
	BrowserURL  string
	OutputDir   string
	TargetOS    string
	TargetArch  string
	DefaultMode string // "service" or "standalone"
	Formats     []string
}

const mainTemplate = `
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"go-deploy/pkg/wrapper"
)

//go:embed embedded_binary
var embeddedBinary embed.FS

const (
	ServiceName = "{{.ServiceName}}"
	DisplayName = "{{.DisplayName}}"
	Description = "{{.Description}}"
	Version     = "{{.Version}}"
	BrowserURL  = "{{.BrowserURL}}"
	DefaultMode = "{{.DefaultMode}}"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("App Version: %s\n", Version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--stop" {
		stopCfg := wrapper.Config{
			Name:        ServiceName,
			DisplayName: DisplayName,
			Description: Description,
			BrowserURL:  BrowserURL,
		}
		if err := wrapper.StopAppliance(stopCfg); err != nil {
			log.Fatalf("Failed to stop app: %v", err)
		}
		return
	}

	tmpFile, err := os.CreateTemp("", "appliance_bin_*")
	if err != nil {
		log.Fatalf("Failed to create temporary binary file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	data, err := embeddedBinary.ReadFile("embedded_binary")
	if err != nil {
		log.Fatalf("Failed to read embedded binary: %v", err)
	}

	if err := os.WriteFile(tmpPath, data, 0755); err != nil {
		log.Fatalf("Failed to write temporary binary: %v", err)
	}
	if err := os.Chmod(tmpPath, 0755); err != nil {
		log.Fatalf("Failed to mark temporary binary executable: %v", err)
	}

	cfg := wrapper.Config{
		Name:        ServiceName,
		DisplayName: DisplayName,
		Description: Description,
		Executable:  filepath.Clean(tmpPath),
		BrowserURL:  BrowserURL,
	}

	isLocal := DefaultMode == "standalone"
	wrapper.RunAppliance(cfg, isLocal)
}
`

func Build(opts BuildOptions) error {
	name := resolvedAppName(opts.Name, opts.SourceDir)
	serviceName := sanitizeServiceName(name)
	displayName := name

	if opts.Description == "" {
		opts.Description = name + " app"
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}
	if opts.DefaultMode == "" {
		opts.DefaultMode = "service"
	}
	if opts.DefaultMode != "service" && opts.DefaultMode != "standalone" {
		return fmt.Errorf("invalid default mode: %s", opts.DefaultMode)
	}
	if strings.TrimSpace(opts.SourceDir) == "" {
		return fmt.Errorf("source directory must be provided")
	}
	if err := validateSourceDir(opts.SourceDir); err != nil {
		return err
	}
	if opts.TargetOS == "" || opts.TargetArch == "" {
		return fmt.Errorf("target os and arch are required")
	}
	if err := validatePackagingPrerequisites(opts.Formats, opts.TargetOS); err != nil {
		return err
	}

	opts.Name = name

	buildDir, err := os.MkdirTemp("", "appliance_build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(buildDir)

	targetPath := filepath.Join(buildDir, "embedded_binary")
	tempOutput := filepath.Join(buildDir, "temp_bin")
	cmd := exec.Command("go", "build", "-o", tempOutput)
	cmd.Dir = opts.SourceDir

	env := os.Environ()
	for k, v := range opts.BuildEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	env = append(env, "GOOS="+opts.TargetOS)
	env = append(env, "GOARCH="+opts.TargetArch)
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build source binary: %v\nOutput: %s", err, string(out))
	}

	input, err := os.ReadFile(tempOutput)
	if err != nil {
		return err
	}
	if err := os.WriteFile(targetPath, input, 0755); err != nil {
		return err
	}

	tmpl, err := template.New("main").Parse(mainTemplate)
	if err != nil {
		return err
	}

	replacements := struct {
		BuildOptions
		ServiceName string
		DisplayName string
	}{
		BuildOptions: opts,
		ServiceName:  serviceName,
		DisplayName:  displayName,
	}

	mainFile, err := os.Create(filepath.Join(buildDir, "main.go"))
	if err != nil {
		return err
	}
	if err := tmpl.Execute(mainFile, replacements); err != nil {
		mainFile.Close()
		return err
	}
	mainFile.Close()

	projectRoot, _ := os.Getwd()

	initCmd := exec.Command("go", "mod", "init", "appliance")
	initCmd.Dir = buildDir
	_, _ = initCmd.CombinedOutput()

	replaceCmd := exec.Command("go", "mod", "edit", "-replace", "go-deploy="+projectRoot)
	replaceCmd.Dir = buildDir
	if out, err := replaceCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod edit failed: %v\nOutput: %s", err, string(out))
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = buildDir
	tidyCmd.Env = append(os.Environ(), "GOOS="+opts.TargetOS, "GOARCH="+opts.TargetArch)
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy failed: %v\nOutput: %s", err, string(out))
	}

	outputBase := sanitizeFileStem(name)
	outputName := fmt.Sprintf("%s-%s-%s", outputBase, opts.TargetOS, opts.TargetArch)
	if opts.TargetOS == "windows" {
		outputName += ".exe"
	}
	outputPath := filepath.Join(opts.OutputDir, outputName)

	cmd = exec.Command("go", "build", "-o", outputPath, "main.go")
	cmd.Dir = buildDir
	cmd.Env = append(os.Environ(), "GOOS="+opts.TargetOS, "GOARCH="+opts.TargetArch)

	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %v\nOutput: %s", err, string(out))
	}

	fmt.Printf("Successfully built app binary at %s\n", outputPath)

	for _, format := range opts.Formats {
		switch format {
		case "binary":
			// Raw binary is already created as outputPath.
		case "deb":
			if opts.TargetOS == "linux" {
				if err := packageDeb(opts, outputPath); err != nil {
					return fmt.Errorf("deb packaging failed: %v", err)
				}
			}
		case "dmg":
			if opts.TargetOS == "darwin" {
				if err := packageDmg(opts, outputPath); err != nil {
					return fmt.Errorf("dmg packaging failed: %v", err)
				}
			}
		case "zip":
			if err := packageZip(opts, outputPath); err != nil {
				return fmt.Errorf("zip packaging failed: %v", err)
			}
		case "exe":
			if opts.TargetOS == "windows" {
				if err := packageExeInstaller(opts, outputPath); err != nil {
					return fmt.Errorf("exe installer packaging failed: %v", err)
				}
			}
		default:
			return fmt.Errorf("unsupported package format: %s", format)
		}
	}

	return nil
}

func packageDeb(opts BuildOptions, binaryPath string) error {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		return fmt.Errorf("dpkg-deb not found in PATH")
	}

	tmpDir, err := os.MkdirTemp("", "deb_build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	packageName := sanitizeDebPackageName(opts.Name)
	installName := sanitizeFileStem(opts.Name)
	version := sanitizeVersion(opts.Version)
	arch := debArchitecture(opts.TargetArch)

	installDir := filepath.Join(tmpDir, "opt", packageName)
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return err
	}
	binPath := filepath.Join(installDir, installName)
	if err := copyFile(binaryPath, binPath, 0755); err != nil {
		return err
	}

	desktopDir := filepath.Join(tmpDir, "usr", "share", "applications")
	if err := os.MkdirAll(desktopDir, 0755); err != nil {
		return err
	}
	desktopContent := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Exec=%s
Terminal=false
Categories=Utility;
`, opts.Name, "/opt/"+packageName+"/"+installName)
	if err := os.WriteFile(filepath.Join(desktopDir, packageName+".desktop"), []byte(desktopContent), 0644); err != nil {
		return err
	}

	debianDir := filepath.Join(tmpDir, "DEBIAN")
	if err := os.MkdirAll(debianDir, 0755); err != nil {
		return err
	}
	controlContent := fmt.Sprintf(`Package: %s
Version: %s
Section: utils
Priority: optional
Architecture: %s
Maintainer: Go-Deploy <contact@example.com>
Description: %s
`, packageName, version, arch, strings.ReplaceAll(opts.Description, "\n", " "))
	if err := os.WriteFile(filepath.Join(debianDir, "control"), []byte(controlContent), 0644); err != nil {
		return err
	}

	postinst := fmt.Sprintf(`#!/bin/sh
set -e
ln -sf /opt/%s/%s /usr/local/bin/%s
`, packageName, installName, installName)
	if err := os.WriteFile(filepath.Join(debianDir, "postinst"), []byte(postinst), 0755); err != nil {
		return err
	}

	prerm := fmt.Sprintf(`#!/bin/sh
set -e
rm -f /usr/local/bin/%s
`, installName)
	if err := os.WriteFile(filepath.Join(debianDir, "prerm"), []byte(prerm), 0755); err != nil {
		return err
	}

	outputName := fmt.Sprintf("%s_%s_%s.deb", packageName, version, arch)
	outputPath := filepath.Join(opts.OutputDir, outputName)
	cmd := exec.Command("dpkg-deb", "--build", tmpDir, outputPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run dpkg-deb: %v\nOutput: %s", err, string(out))
	}

	fmt.Printf("Successfully packaged .deb at %s\n", outputPath)
	return nil
}

func packageDmg(opts BuildOptions, binaryPath string) error {
	if _, err := exec.LookPath("hdiutil"); err != nil {
		return fmt.Errorf("hdiutil not found in PATH")
	}

	name := sanitizeFileStem(opts.Name)
	outputName := fmt.Sprintf("%s-%s-%s.dmg", name, opts.TargetOS, opts.TargetArch)
	outputPath := filepath.Join(opts.OutputDir, outputName)

	tmpDir, err := os.MkdirTemp("", "dmg_build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	appBundleName := opts.Name + ".app"
	appBundleRoot := filepath.Join(tmpDir, appBundleName)
	macOSDir := filepath.Join(appBundleRoot, "Contents", "MacOS")
	if err := os.MkdirAll(macOSDir, 0755); err != nil {
		return err
	}

	appExecName := sanitizeFileStem(opts.Name)
	appExecPath := filepath.Join(macOSDir, appExecName)
	if err := copyFile(binaryPath, appExecPath, 0755); err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>%s</string>
	<key>CFBundleDisplayName</key>
	<string>%s</string>
	<key>CFBundleIdentifier</key>
	<string>com.godeploy.%s</string>
	<key>CFBundleVersion</key>
	<string>%s</string>
	<key>CFBundleShortVersionString</key>
	<string>%s</string>
	<key>CFBundleExecutable</key>
	<string>%s</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>LSBackgroundOnly</key>
	<true/>
</dict>
</plist>
`, opts.Name, opts.Name, strings.ToLower(sanitizeFileStem(opts.Name)), opts.Version, opts.Version, appExecName)
	if err := os.WriteFile(filepath.Join(appBundleRoot, "Contents", "Info.plist"), []byte(plist), 0644); err != nil {
		return err
	}

	_ = os.Symlink("/Applications", filepath.Join(tmpDir, "Applications"))

	cmd := exec.Command("hdiutil", "create", "-volname", opts.Name, "-srcfolder", tmpDir, "-ov", "-format", "UDZO", outputPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("hdiutil failed: %v\nOutput: %s", err, string(out))
	}

	fmt.Printf("Successfully packaged .dmg at %s\n", outputPath)
	return nil
}

func packageZip(opts BuildOptions, binaryPath string) error {
	name := sanitizeFileStem(opts.Name)
	outputName := fmt.Sprintf("%s-%s-%s.zip", name, opts.TargetOS, opts.TargetArch)
	outputPath := filepath.Join(opts.OutputDir, outputName)

	zipFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	binData, err := os.ReadFile(binaryPath)
	if err != nil {
		return err
	}

	entryName := filepath.Base(binaryPath)
	if runtime.GOOS == "windows" {
		entryName = strings.ReplaceAll(entryName, "\\", "/")
	}
	w, err := zw.Create(entryName)
	if err != nil {
		return err
	}
	if _, err := w.Write(binData); err != nil {
		return err
	}

	fmt.Printf("Successfully packaged .zip at %s\n", outputPath)
	return nil
}

func packageExeInstaller(opts BuildOptions, binaryPath string) error {
	if _, err := exec.LookPath("makensis"); err != nil {
		return fmt.Errorf("makensis not found in PATH; install NSIS to generate Windows setup .exe")
	}

	tmpDir, err := os.MkdirTemp("", "exe_setup")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	binaryName := sanitizeFileStem(opts.Name) + ".exe"
	if err := copyFile(binaryPath, filepath.Join(tmpDir, binaryName), 0755); err != nil {
		return err
	}

	outputName := fmt.Sprintf("%s-%s-%s-setup.exe", sanitizeFileStem(opts.Name), opts.TargetOS, opts.TargetArch)
	outputPath := filepath.Join(opts.OutputDir, outputName)

	script := fmt.Sprintf(`!define APP_NAME "%s"
!define APP_EXE "%s"
OutFile "%s"
InstallDir "$PROGRAMFILES64\\${APP_NAME}"
RequestExecutionLevel admin

Page directory
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

Section "Install"
  SetOutPath "$INSTDIR"
  File "${APP_EXE}"
  CreateDirectory "$SMPROGRAMS\\${APP_NAME}"
  CreateShortcut "$SMPROGRAMS\\${APP_NAME}\\${APP_NAME}.lnk" "$INSTDIR\\${APP_EXE}"
  CreateShortcut "$DESKTOP\\${APP_NAME}.lnk" "$INSTDIR\\${APP_EXE}"
  WriteUninstaller "$INSTDIR\\Uninstall.exe"
SectionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\\${APP_NAME}\\${APP_NAME}.lnk"
  RMDir "$SMPROGRAMS\\${APP_NAME}"
  Delete "$DESKTOP\\${APP_NAME}.lnk"
  Delete "$INSTDIR\\${APP_EXE}"
  Delete "$INSTDIR\\Uninstall.exe"
  RMDir "$INSTDIR"
SectionEnd
`, escapeNSISString(opts.Name), escapeNSISString(binaryName), escapeNSISString(filepath.ToSlash(outputPath)))

	scriptPath := filepath.Join(tmpDir, "installer.nsi")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return err
	}

	cmd := exec.Command("makensis", "/V2", scriptPath)
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("makensis failed: %v\nOutput: %s", err, string(out))
	}

	fmt.Printf("Successfully packaged setup .exe at %s\n", outputPath)
	return nil
}

func resolvedAppName(name string, sourceDir string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed != "" {
		return trimmed
	}
	base := filepath.Base(filepath.Clean(sourceDir))
	if base == "." || base == string(filepath.Separator) || strings.TrimSpace(base) == "" {
		return "go-app"
	}
	return base
}

func sanitizeServiceName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_]+`)
	s := re.ReplaceAllString(name, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "go_app"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "app_" + s
	}
	return s
}

func sanitizeDebPackageName(name string) string {
	re := regexp.MustCompile(`[^a-z0-9+.-]+`)
	s := strings.ToLower(strings.TrimSpace(name))
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "go-app"
	}
	if s[0] < 'a' || s[0] > 'z' {
		s = "app-" + s
	}
	return s
}

func sanitizeFileStem(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	s := strings.TrimSpace(name)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "go-app"
	}
	return s
}

func sanitizeVersion(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return "1.0.0"
	}
	return trimmed
}

func debArchitecture(targetArch string) string {
	switch targetArch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "i386"
	default:
		return targetArch
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

func escapeNSISString(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "$\\\"")
	return v
}

func validateSourceDir(sourceDir string) error {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("source directory error: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source directory is not a directory: %s", sourceDir)
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "go.mod")); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source directory must contain a go.mod file: %s", sourceDir)
		}
		return fmt.Errorf("failed checking go.mod in source directory: %w", err)
	}
	return nil
}

func validatePackagingPrerequisites(formats []string, targetOS string) error {
	missing := missingTools(formats, targetOS)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required packaging tools: %s", strings.Join(missing, ", "))
}

func missingTools(formats []string, targetOS string) []string {
	required := requiredToolsForPackaging(formats, targetOS)
	var missing []string
	for _, tool := range required {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing
}

func requiredToolsForPackaging(formats []string, targetOS string) []string {
	needed := make(map[string]struct{})
	for _, format := range formats {
		switch format {
		case "deb":
			if targetOS == "linux" {
				needed["dpkg-deb"] = struct{}{}
			}
		case "dmg":
			if targetOS == "darwin" {
				needed["hdiutil"] = struct{}{}
			}
		case "exe":
			if targetOS == "windows" {
				needed["makensis"] = struct{}{}
			}
		}
	}

	var required []string
	for tool := range needed {
		required = append(required, tool)
	}
	sort.Strings(required)
	return required
}
