package daemon

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Label is the LaunchAgent label.
const Label = "com.andymartinezot.avocado"

func agentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// LogPath is where the LaunchAgent writes the daemon's log.
func LogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "avocado.log")
}

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// InstalledBinary is where Install copies the executable. It must live outside
// privacy-protected folders (~/Documents, ~/Desktop, ~/Downloads): a launchd
// agent can't show the permission prompt and would hang reading its own binary.
func InstalledBinary() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "AvocadoMTP", "bin", "avocado")
}

// Install copies exe to InstalledBinary, writes a LaunchAgent that runs it at
// login and keeps it alive, then starts it.
func Install(exe string) (string, error) {
	stop() // the running daemon holds the old binary
	exe, err := copyBinary(exe, InstalledBinary())
	if err != nil {
		return "", fmt.Errorf("install binary: %w", err)
	}
	x := html.EscapeString
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>daemon</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, Label, x(exe), x(LogPath()), x(LogPath()))
	p := agentPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
		return "", err
	}
	// launchd may still be tearing down the old instance; retry briefly.
	for i := 0; i < 20; i++ {
		if err = launchctl("bootstrap", domain(), p); err == nil {
			return p, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", err
}

// copyBinary atomically replaces dst with a copy of src and returns dst.
func copyBinary(src, dst string) (string, error) {
	src, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", err
	}
	if src == dst {
		return dst, nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

// stop unloads the agent and waits until launchd has let go of it (the daemon
// unmounts its volumes on the way out, which can take a few seconds).
func stop() {
	if launchctl("bootout", domain()+"/"+Label) != nil {
		return // not loaded
	}
	for i := 0; i < 60; i++ {
		if launchctl("print", domain()+"/"+Label) != nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Uninstall stops the LaunchAgent and removes its plist.
func Uninstall() error {
	stop()
	if err := os.Remove(agentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(InstalledBinary())
	return nil
}

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
