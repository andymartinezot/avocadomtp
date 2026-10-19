package daemon

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Label is the LaunchAgent label.
const Label = "com.andymartinezot.avocadomtp"

// legacyLabel is the agent installed by versions named "avocado".
const legacyLabel = "com.andymartinezot.avocado"

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func agentPath() string { return filepath.Join(home(), "Library", "LaunchAgents", Label+".plist") }

// LogPath is where the LaunchAgent writes the daemon's log.
func LogPath() string { return filepath.Join(home(), "Library", "Logs", "avocadomtp.log") }

func domain() string  { return fmt.Sprintf("gui/%d", os.Getuid()) }
func service() string { return domain() + "/" + Label }

// InstalledBinary is where Install copies the executable. It must live outside
// privacy-protected folders (~/Documents, ~/Desktop, ~/Downloads): a launchd
// agent can't show the permission prompt and would hang reading its own binary.
func InstalledBinary() string {
	return filepath.Join(home(), "Library", "Application Support", "AvocadoMTP", "bin", "avocadomtp")
}

// AgentState describes the LaunchAgent.
type AgentState struct {
	Installed bool // the plist exists
	Running   bool // the daemon process is alive
	PID       int
	Autostart bool // launchd starts it at login
}

// State reports whether the agent is installed, running and set to start at login.
func State() AgentState {
	st := AgentState{Autostart: autostartEnabled()}
	if _, err := os.Stat(agentPath()); err == nil {
		st.Installed = true
	}
	out, err := exec.Command("launchctl", "print", service()).Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "pid = ") {
				st.PID, _ = strconv.Atoi(strings.TrimPrefix(line, "pid = "))
				st.Running = st.PID > 0
			}
		}
	}
	return st
}

// Install copies exe to InstalledBinary, writes the LaunchAgent and, if start
// is set, starts it. The start-at-login preference is left as the user set it
// (default: on).
func Install(exe string, start bool) (string, error) {
	removeLegacy()
	_ = Stop() // the running daemon holds the old binary
	bin, err := copyBinary(exe, InstalledBinary())
	if err != nil {
		return "", fmt.Errorf("install binary: %w", err)
	}
	x := html.EscapeString
	// KeepAlive/SuccessfulExit=false: restart after a crash or error exit, but
	// not after a deliberate stop (the daemon exits 0 on SIGTERM).
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>daemon</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, Label, x(bin), x(LogPath()), x(LogPath()))
	p := agentPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
		return "", err
	}
	if !start {
		return p, nil
	}
	return p, Start()
}

// Start launches the daemon now. Turning autostart off doesn't prevent this.
func Start() error {
	if _, err := os.Stat(agentPath()); err != nil {
		return fmt.Errorf("the daemon is not installed (run `avocadomtp daemon install`)")
	}
	if State().Running {
		return nil
	}
	auto := autostartEnabled()
	if !auto {
		// A disabled service can't be loaded: enable it just for this load.
		_ = launchctl("enable", service())
		defer launchctl("disable", service())
	}
	var err error
	for i := 0; i < 20; i++ { // launchd may still be tearing down a previous instance
		if err = launchctl("bootstrap", domain(), agentPath()); err == nil || State().Running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

// Stop shuts the daemon down (it unmounts its volumes first) and keeps it
// stopped until Start or the next login (if autostart is on).
func Stop() error {
	if launchctl("bootout", service()) != nil {
		return nil // not loaded
	}
	for i := 0; i < 80; i++ {
		if launchctl("print", service()) != nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("the daemon did not stop within 20 s")
}

// SetAutostart chooses whether the daemon starts at login. It doesn't start or
// stop the daemon now.
func SetAutostart(on bool) error {
	if on {
		return launchctl("enable", service())
	}
	return launchctl("disable", service())
}

// autostartEnabled reads launchd's persistent enable/disable overrides.
func autostartEnabled() bool {
	out, err := exec.Command("launchctl", "print-disabled", domain()).Output()
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, `"`+Label+`"`) {
			return !strings.Contains(line, "disabled") && !strings.Contains(line, "true")
		}
	}
	return true // no override: enabled
}

// Uninstall stops the daemon and removes everything the service installed:
// the agent, its binary, the socket, the write-staging cache and the log.
func Uninstall() error {
	_ = Stop()
	_ = launchctl("enable", service()) // drop our autostart override
	if err := os.Remove(agentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	appSupport := filepath.Dir(filepath.Dir(InstalledBinary()))
	_ = os.Remove(InstalledBinary())
	_ = os.Remove(filepath.Dir(InstalledBinary()))
	_ = os.Remove(filepath.Join(appSupport, "avocadomtp.sock"))
	_ = os.Remove(appSupport) // only if empty: never delete files we don't know
	_ = os.RemoveAll(CacheDir())
	_ = os.Remove(LogPath())
	return removeCLILink(CLILink, InstalledBinary())
}

// CLILink is where the menu bar app's "Install command-line tool…" puts a
// symlink to InstalledBinary.
const CLILink = "/usr/local/bin/avocadomtp"

// removeCLILink deletes link only if it is a symlink to target, so another
// tool's avocadomtp is never touched. /usr/local/bin is usually root-owned,
// so it falls back to the macOS administrator prompt.
func removeCLILink(link, target string) error {
	if dest, err := os.Readlink(link); err != nil || dest != target {
		return nil
	}
	err := os.Remove(link)
	if err == nil || !os.IsPermission(err) {
		return err
	}
	script := fmt.Sprintf(`do shell script "rm -f '%s'" with administrator privileges`, link)
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		return fmt.Errorf("remove %s: %v: %s", link, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CacheDir holds the Finder volumes' write-staging files.
func CacheDir() string { return filepath.Join(home(), "Library", "Caches", "AvocadoMTP") }

// AppPath is where the menu bar app is installed.
const AppPath = "/Applications/AvocadoMTP.app"

// UninstallApp removes the menu bar app: it unregisters its own login item and
// clears its preferences (only the app can do that), then the bundle is deleted.
func UninstallApp() (bool, error) {
	if _, err := os.Stat(AppPath); err != nil {
		return false, nil
	}
	_ = exec.Command(filepath.Join(AppPath, "Contents", "MacOS", "AvocadoMTP"), "--uninstall").Run()
	_ = exec.Command("pkill", "-x", "AvocadoMTP").Run()
	if err := os.RemoveAll(AppPath); err != nil {
		return true, fmt.Errorf("remove %s: %w", AppPath, err)
	}
	return true, nil
}

// removeLegacy cleans up an agent installed under the old "avocado" name.
func removeLegacy() {
	_ = launchctl("bootout", domain()+"/"+legacyLabel)
	appSupport := filepath.Join(home(), "Library", "Application Support", "AvocadoMTP")
	for _, p := range []string{
		filepath.Join(home(), "Library", "LaunchAgents", legacyLabel+".plist"),
		filepath.Join(appSupport, "bin", "avocado"),
		filepath.Join(appSupport, "avocado.sock"),
		filepath.Join(home(), "Library", "Logs", "avocado.log"),
	} {
		_ = os.Remove(p)
	}
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

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
