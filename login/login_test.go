package login

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableDisable(t *testing.T) {
	home := t.TempDir()
	if Enabled(home) {
		t.Fatal("enabled before Enable")
	}
	if err := Disable(home); err != nil {
		t.Fatalf("Disable when off: %v", err)
	}
	exe := "/Users/me/bin/snow & fall"
	if err := Enable(home, exe); err != nil {
		t.Fatal(err)
	}
	if !Enabled(home) {
		t.Fatal("not enabled after Enable")
	}
	data, err := os.ReadFile(PlistPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<string>/Users/me/bin/snow &amp; fall</string>") {
		t.Errorf("program path missing or unescaped:\n%s", data)
	}
	// Re-enabling points it at a new path, leaving no temp files behind.
	if err := Enable(home, "/Applications/CokeEverywhere.app/Contents/MacOS/CokeEverywhere"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(PlistPath(home))
	if strings.Contains(string(data), "snow &amp; fall") || !strings.Contains(string(data), "/Applications/CokeEverywhere.app/Contents/MacOS/CokeEverywhere") {
		t.Errorf("path not updated:\n%s", data)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(PlistPath(home)), "*.tmp")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	if err := Disable(home); err != nil || Enabled(home) {
		t.Fatalf("Disable: err %v, still enabled %v", err, Enabled(home))
	}
}

func TestEnableRejectsRelativePath(t *testing.T) {
	if err := Enable(t.TempDir(), "./CokeEverywhere"); err == nil {
		t.Error("relative path accepted")
	}
}

// The plist must be one launchd accepts; plutil is the system's checker.
func TestPlistIsValid(t *testing.T) {
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		t.Skip("plutil not available")
	}
	home := t.TempDir()
	if err := Enable(home, "/Applications/CokeEverywhere.app/Contents/MacOS/CokeEverywhere"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(plutil, "-lint", PlistPath(home)).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v\n%s", err, out)
	}
}
