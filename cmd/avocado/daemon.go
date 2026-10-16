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
		p, err := daemon.Install(exe)
		if err != nil {
			return err
		}
		fmt.Printf("installed %s\nthe daemon now starts at login; log: %s\n", p, daemon.LogPath())
		return nil
	case "uninstall":
		if err := daemon.Uninstall(); err != nil {
			return err
		}
		fmt.Println("daemon stopped and removed from login items")
		return nil
	case "status":
		return daemonStatus(socket)
	}
	return fmt.Errorf("unknown daemon command %q (run, install, uninstall, status)", sub)
}

func runDaemon(serial, socket string, mount bool) error {
	ln, err := daemon.Listen(socket)
	if err != nil {
		return err
	}
	logger := log.New(os.Stderr, "avocado: ", log.LstdFlags)
	logger.Printf("listening on %s", socket)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &daemon.Server{
		Log:  logger,
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
	if os.Getenv("AVOCADO_MOUNT_ROOT") != "" {
		m.Root = os.Getenv("AVOCADO_MOUNT_ROOT")
	} else {
		m.FSKit = fusefs.FSKitEnabled()
	}
	if os.Getenv("AVOCADO_FUSE_DEBUG") != "" {
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
			return fmt.Errorf("daemon not running (start it with `avocado daemon` or `avocado daemon install`)")
		}
		return err
	}
	defer c.Close()
	st, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("daemon:   running (%s)\n", socket)
	if !st.Connected {
		fmt.Printf("phone:    not connected (%s)\n", st.Error)
		return nil
	}
	fmt.Printf("phone:    %s %s (serial %s)\n", st.Device.Manufacturer, st.Device.Model, st.Device.Serial)
	for _, s := range st.Storages {
		ro := ""
		if s.ReadOnly {
			ro = " (read-only)"
		}
		fmt.Printf("storage:  %-28s id %08X  %s free of %s%s\n", s.Name, s.ID, human(int64(s.Free)), human(int64(s.Capacity)), ro)
	}
	return nil
}
