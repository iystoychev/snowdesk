// Package login turns "start at login" on and off with a per-user
// LaunchAgent: a plist in ~/Library/LaunchAgents that launchd reads at
// login. It needs no app bundle and no permissions.
package login

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Label names the LaunchAgent; it is also the plist's file name.
const Label = "local.snowfall"

// PlistPath is where the LaunchAgent lives for the user with this home dir.
func PlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// Plist is the LaunchAgent that starts exe once at login, in the GUI session.
func Plist(exe string) []byte {
	var esc bytes.Buffer
	xml.EscapeText(&esc, []byte(exe))
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, Label, esc.String()))
}

// Enabled reports whether start at login is on.
func Enabled(home string) bool {
	_, err := os.Stat(PlistPath(home))
	return err == nil
}

// Enable turns start at login on for exe, or points it at exe if it was
// already on. The file is replaced atomically.
func Enable(home, exe string) error {
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("start at login needs an absolute program path, got %q", exe)
	}
	path := PlistPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), Label+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(Plist(exe)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Disable turns start at login off. It does not stop the running app.
func Disable(home string) error {
	err := os.Remove(PlistPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
