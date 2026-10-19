package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/andymartinezot/avocadomtp/internal/activity"
	"github.com/andymartinezot/avocadomtp/internal/daemon"
	"github.com/andymartinezot/avocadomtp/internal/device"
	"github.com/andymartinezot/avocadomtp/internal/fusefs"
	"github.com/andymartinezot/avocadomtp/internal/usbx"
)

func daemonCmd(serial string, args []string) error {
	sub := "run"
	if len(args) > 0 {
		sub = args[0]
	}
	socket := daemon.DefaultSocketPath()
	switch sub {
	case "run", "--no-mount":
		return runDaemon(serial, socket, sub != "--no-mount" && !(len(args) > 1 && args[1] == "--no-mount"))
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		start := !(len(args) > 1 && args[1] == "--no-start")
		p, err := daemon.Install(exe, start)
		if err != nil {
			return err
		}
		if start {
			fmt.Printf("installed %s and started the daemon\nlog: %s\n", p, daemon.LogPath())
		} else {
			fmt.Printf("installed %s (not started)\nlog: %s\n", p, daemon.LogPath())
		}
		return printAgent()
	case "uninstall":
		if err := daemon.Uninstall(); err != nil {
			return err
		}
		fmt.Println("service stopped and removed (agent, binary, socket, cache, log)")
		return nil
	case "start":
		if err := daemon.Start(); err != nil {
			return err
		}
		return printAgent()
	case "stop":
		if err := daemon.Stop(); err != nil {
			return err
		}
		return printAgent()
	case "autostart":
		if len(args) < 2 || (args[1] != "on" && args[1] != "off") {
			return fmt.Errorf("usage: avocadomtp daemon autostart on|off")
		}
		if err := daemon.SetAutostart(args[1] == "on"); err != nil {
			return err
		}
		return printAgent()
	case "status":
		if err := printAgent(); err != nil {
			return err
		}
		fuseT := fusefs.Available()
		fmt.Println(volumesLine(fuseT, fuseT && fusefs.FSKitEnabled()))
		return daemonStatus(socket)
	}
	return fmt.Errorf("unknown daemon command %q (run, install, uninstall, start, stop, autostart, status)", sub)
}

// volumesLine reports how Finder volumes will mount; the menu bar app reads it
// to show its setup checklist.
func volumesLine(fuseT, fskit bool) string {
	switch {
	case !fuseT:
		return "volumes:   unavailable (FUSE-T not installed)"
	case fskit:
		return "volumes:   fskit"
	default:
		return "volumes:   nfs (enable fuse-t in File System Extensions for local disks)"
	}
}

func printAgent() error {
	st := daemon.State()
	switch {
	case !st.Installed:
		fmt.Println("daemon:    not installed (avocadomtp daemon install)")
	case st.Running:
		fmt.Printf("daemon:    running (pid %d)\n", st.PID)
	default:
		fmt.Println("daemon:    stopped")
	}
	auto := "off"
	if st.Autostart {
		auto = "on"
	}
	fmt.Printf("autostart: %s (start at login)\n", auto)
	return nil
}

func runDaemon(serial, socket string, mount bool) error {
	ln, err := daemon.Listen(socket)
	if err != nil {
		return err
	}
	logger := log.New(os.Stderr, "avocadomtp: ", log.LstdFlags)
	logger.Printf("listening on %s", socket)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hub := activity.NewHub()
	srv := &daemon.Server{
		Log:  logger,
		Hub:  hub,
		Open: func() (daemon.Phone, error) { return device.Open(serial) },
	}
	if changes, err := usbx.WatchHotplug(ctx); err == nil {
		srv.Changes = changes
	} else {
		logger.Printf("USB hotplug unavailable (%v); polling for the phone instead", err)
	}

	var mounts *fusefs.Manager
	switch {
	case !mount:
	case !fusefs.Available():
		logger.Printf("Finder volumes disabled: FUSE-T is not installed (brew install --cask fuse-t)")
	default:
		mounts = newMountManager(logger)
		mounts.Activity = hub
		srv.Eject = mounts.Eject
		srv.Remount = func() error { return mounts.Remount(ctx, srv.Remote()) }
		srv.OnConnect = func() {
			if err := mounts.MountAll(ctx, srv.Remote()); err != nil {
				logger.Printf("mount: %v", err)
			}
		}
		srv.OnDisconnect = mounts.UnmountAll
	}

	err = srv.Serve(ctx, ln)
	if mounts != nil {
		mounts.UnmountAll()
	}
	os.Remove(socket)
	logger.Printf("stopped")
	return err
}

// newMountManager mounts in ~/AvocadoMTP, through FUSE-T's FSKit backend
// when its module is enabled and its NFS backend otherwise.
func newMountManager(logger *log.Logger) *fusefs.Manager {
	home, _ := os.UserHomeDir()
	m := &fusefs.Manager{
		Root:   filepath.Join(home, "AvocadoMTP"),
		TmpDir: filepath.Join(home, "Library", "Caches", "AvocadoMTP", "staging"),
		Log:    logger,
	}
	if os.Getenv("AVOCADOMTP_MOUNT_ROOT") != "" {
		m.Root = os.Getenv("AVOCADOMTP_MOUNT_ROOT")
	} else {
		m.FSKit = fusefs.FSKitEnabled()
	}
	if os.Getenv("AVOCADOMTP_FUSE_DEBUG") != "" {
		m.Debug = logger
	}
	if m.FSKit {
		logger.Printf("Finder volumes will be mounted in %s (FUSE-T FSKit: local disks)", m.Root)
	} else {
		logger.Printf("Finder volumes will be mounted in %s (FUSE-T NFS); enable fuse-t in System Settings → "+
			"General → Login Items & Extensions → File System Extensions for real disks", m.Root)
	}
	return m
}

func daemonStatus(socket string) error {
	c, err := daemon.Dial(socket)
	if err != nil {
		if errors.Is(err, daemon.ErrNotRunning) {
			return nil // printAgent already said it's stopped
		}
		return err
	}
	defer c.Close()
	st, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	if !st.Connected {
		fmt.Printf("phone:     not connected (%s)\n", printable(st.Error))
		return nil
	}
	fmt.Printf("phone:     %s %s (serial %s)\n", printable(st.Device.Manufacturer), printable(st.Device.Model), printable(st.Device.Serial))
	for _, s := range st.Storages {
		ro := ""
		if s.ReadOnly {
			ro = " (read-only)"
		}
		fmt.Printf("storage:   %-26s %9s free of %s%s\n", s.Name, human(int64(s.Free)), human(int64(s.Capacity)), ro)
	}
	for _, m := range st.Mounts {
		fmt.Printf("mounted:   %s (%s)\n", m.Path, m.Backend)
	}
	if s := st.Stats; s != nil {
		fmt.Printf("traffic:   %s in · %s out · %d MTP operations\n", human(int64(s.BytesIn)), human(int64(s.BytesOut)), s.Ops)
	}
	return nil
}
