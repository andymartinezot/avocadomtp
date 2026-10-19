package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/activity"
	"github.com/andymartinezot/avocadomtp/internal/daemon"
)

func needDaemon(err error) error {
	if errors.Is(err, daemon.ErrNotRunning) {
		return errors.New("the daemon is not running (avocadomtp daemon start)")
	}
	return err
}

// watchCmd prints the daemon's activity until Ctrl-C.
func watchCmd() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	tty := stderrIsTTY
	err := daemon.Watch(ctx, daemon.DefaultSocketPath(), func(st activity.State) {
		if tty {
			fmt.Print("\033[H\033[2J") // redraw in place
		}
		fmt.Print(renderState(st))
		if !tty {
			fmt.Println("---")
		}
	})
	return needDaemon(err)
}

func renderState(st activity.State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "AvocadoMTP · %s\n\n", time.Now().Format("15:04:05"))
	switch {
	case st.Connected && st.Device != nil:
		fmt.Fprintf(&b, "Phone:   %s %s\n", printable(st.Device.Manufacturer), printable(st.Device.Model))
	case st.Error != "":
		fmt.Fprintf(&b, "Phone:   not connected (%s)\n", printable(st.Error))
	default:
		fmt.Fprintf(&b, "Phone:   not connected\n")
	}
	if len(st.Mounts) == 0 {
		fmt.Fprintf(&b, "Volumes: none\n")
	}
	for _, m := range st.Mounts {
		fmt.Fprintf(&b, "Volume:  %-24s %s (%s)\n", m.Name, m.Path, m.Backend)
	}
	fmt.Fprintf(&b, "Speed:   ↓ %s/s from phone · ↑ %s/s to phone\n", human(st.RateIn), human(st.RateOut))
	if len(st.Transfers) > 0 {
		fmt.Fprintf(&b, "\nTransfers:\n")
	}
	for _, t := range st.Transfers {
		arrow := "↓"
		if t.Direction == activity.ToPhone {
			arrow = "↑"
		}
		pct := ""
		if t.Total > 0 {
			pct = fmt.Sprintf("%3.0f%%  ", float64(t.Bytes)*100/float64(t.Total))
		}
		state := fmt.Sprintf("%s/s", human(t.Rate))
		if t.State != "active" {
			state = t.State
			if t.Error != "" {
				state += ": " + printable(t.Error)
			}
		}
		fmt.Fprintf(&b, "  %s %s%-40s %9s  %s\n", arrow, pct, truncate(t.Path, 40), human(t.Bytes), state)
	}
	return b.String()
}

// ejectCmd unmounts the named storage.
func ejectCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: avocadomtp eject STORAGE|--all (e.g. Retroid6)")
	}
	all := args[0] == "--all"
	c, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return needDaemon(err)
	}
	defer c.Close()
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if all {
		for _, m := range st.Mounts {
			if err := c.Eject(ctx, m.StorageID); err != nil {
				return err
			}
			fmt.Printf("ejected %s\n", m.Path)
		}
		if len(st.Mounts) == 0 {
			fmt.Println("nothing mounted")
		}
		return nil
	}
	for _, m := range st.Mounts {
		if strings.EqualFold(m.Name, strings.Trim(args[0], "/")) {
			if err := c.Eject(ctx, m.StorageID); err != nil {
				return err
			}
			fmt.Printf("ejected %s\n", m.Path)
			return nil
		}
	}
	return fmt.Errorf("no mounted volume named %q (see avocadomtp daemon status)", args[0])
}

// mountCmd mounts every storage again.
func mountCmd() error {
	c, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return needDaemon(err)
	}
	defer c.Close()
	ctx := context.Background()
	if err := c.Remount(ctx); err != nil {
		return err
	}
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	for _, m := range st.Mounts {
		fmt.Printf("mounted %s\n", m.Path)
	}
	return nil
}

// uninstallAll removes everything AvocadoMTP installed on this Mac: the
// service (agent, binary, socket, cache, log) and the menu bar app (login
// item, preferences, bundle). Prerequisites (FUSE-T, libusb) are left alone.
func uninstallAll() error {
	if err := daemon.Uninstall(); err != nil {
		return err
	}
	fmt.Println("service: removed (agent, binary, socket, cache, log)")
	removed, err := daemon.UninstallApp()
	switch {
	case err != nil:
		return err
	case removed:
		fmt.Println("menu bar app: removed (login item, preferences, " + daemon.AppPath + ")")
	default:
		fmt.Println("menu bar app: not installed")
	}
	fmt.Println("FUSE-T and libusb were left installed; remove them with Homebrew if you no longer need them.")
	return nil
}
