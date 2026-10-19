// Command avocadomtp transfers files between macOS and Android phones over USB (MTP).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"strings"

	"github.com/andymartinezot/avocadomtp/internal/device"
	"github.com/andymartinezot/avocadomtp/internal/vfs"
)

const usage = `avocadomtp — Android file transfer over USB (MTP)

Usage: avocadomtp [-d SERIAL] [--direct] <command> [args]

Commands:
  devices                         list connected MTP devices
  info                            show device model and storages
  ls [-l] [PATH]                  list a folder (default /)
  pull [-r] REMOTE [LOCAL]        copy from the phone (default: current dir)
  push [-r] LOCAL... REMOTE_DIR   copy to the phone
  rm [-r] PATH...                 delete files (or folders with -r)
  mkdir [-p] PATH...              create folders
  mv SRC DST                      rename or move
  shell                           interactive session (cd, ls, pull, push, ...)
  watch                           live view: phone, mounts, transfers and speed
  eject STORAGE | --all           unmount one or every Finder volume (until "mount" or reconnect)
  mount                           mount all Finder volumes again
  uninstall                       remove the service and the menu bar app (keeps your phone's files)

Background daemon (owns the USB connection; used by the CLI and Finder):
  daemon [--no-mount]             run in the foreground; mounts each storage as a
                                  Finder volume (needs FUSE-T) unless --no-mount
  daemon install | uninstall      set up the background service (and start it) / remove it
  daemon start | stop             start / stop the background service now
  daemon autostart on|off         start the service at login (default on)
  daemon status                   service state, phone, mounts and traffic

When the daemon runs, commands go through it; --direct talks to USB instead.
Remote paths start with the storage name, e.g.
  avocadomtp ls -l "/Internal shared storage/DCIM/Camera"
  avocadomtp push -r ~/Music "/SD card/Music"
Flags may appear anywhere; use -- before a path that starts with "-".
`

func main() {
	args := os.Args[1:]
	serial, direct := "", false
	for len(args) > 0 {
		if len(args) >= 2 && args[0] == "-d" {
			serial, args = args[1], args[2:]
		} else if args[0] == "--direct" {
			direct, args = true, args[1:]
		} else {
			break
		}
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		return
	}
	if err := runMain(serial, direct, args); err != nil {
		fmt.Fprintln(os.Stderr, "avocadomtp:", err)
		os.Exit(1)
	}
}

func runMain(serial string, direct bool, args []string) error {
	switch args[0] {
	case "devices":
		return listDevices(os.Stdout)
	case "daemon":
		return daemonCmd(serial, args[1:])
	case "watch":
		return watchCmd()
	case "eject":
		return ejectCmd(args[1:])
	case "mount":
		return mountCmd()
	case "uninstall":
		return uninstallAll()
	}
	if _, ok := commands[args[0]]; !ok && args[0] != "shell" {
		return fmt.Errorf("unknown command %q (see avocadomtp --help)", args[0])
	}
	c, err := connect(serial, direct)
	if err != nil {
		return err
	}
	defer c.close()
	if args[0] == "shell" {
		return c.shell()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return c.run(ctx, os.Stdout, "/", args)
}

func listDevices(w io.Writer) error {
	cands, err := device.List()
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		return device.ErrNoDevice
	}
	for _, c := range cands {
		fmt.Fprintf(w, "%-16s %s\n", c.Serial, c)
	}
	return nil
}

type command struct {
	flags   string // allowed single-letter flags
	minArgs int
	maxArgs int // -1 = unlimited
	run     func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error
}

var commands = map[string]command{
	"info": {"", 0, 0, func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error {
		return c.info(ctx, w)
	}},
	"ls": {"l", 0, 1, func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error {
		p := "/"
		if len(a) > 0 {
			p = a[0]
		}
		return c.ls(ctx, w, p, f['l'])
	}},
	"pull": {"r", 1, 2, func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error {
		local := ""
		if len(a) > 1 {
			local = a[1]
		}
		return c.pull(ctx, a[0], local, f['r'])
	}},
	"push": {"r", 2, -1, func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error {
		return c.push(ctx, a[:len(a)-1], a[len(a)-1], f['r'])
	}},
	"rm": {"rf", 1, -1, func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error {
		for _, p := range a {
			if err := c.rm(ctx, w, p, f['r']); err != nil {
				return err
			}
		}
		return nil
	}},
	"mkdir": {"p", 1, -1, func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error {
		for _, p := range a {
			if err := c.mkdir(ctx, p, f['p']); err != nil {
				return err
			}
		}
		return nil
	}},
	"mv": {"", 2, 2, func(ctx context.Context, c *client, w io.Writer, f map[rune]bool, a []string) error {
		return c.mv(ctx, a[0], a[1])
	}},
}

// remoteArgs lists, per command, which positional arguments are remote paths
// (resolved against the shell's working folder). -1 means "the last one".
var remoteArgs = map[string][]int{
	"ls": {0}, "pull": {0}, "push": {-1}, "rm": {}, "mkdir": {}, "mv": {0, 1},
}

// run executes one command; remote paths are resolved relative to cwd.
func (c *client) run(ctx context.Context, w io.Writer, cwd string, args []string) error {
	cmd, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q", args[0])
	}
	flags, rest, err := parseFlags(args[1:], cmd.flags)
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	if len(rest) < cmd.minArgs || (cmd.maxArgs >= 0 && len(rest) > cmd.maxArgs) {
		return fmt.Errorf("%s: wrong number of arguments (see help)", args[0])
	}
	idx, ok := remoteArgs[args[0]]
	if ok && len(idx) == 0 { // every argument is remote
		for i := range rest {
			rest[i] = resolve(cwd, rest[i])
		}
	}
	for _, i := range idx {
		if i < 0 {
			i = len(rest) + i
		}
		if i >= 0 && i < len(rest) {
			rest[i] = resolve(cwd, rest[i])
		}
	}
	if args[0] == "ls" && len(rest) == 0 {
		rest = []string{cwd}
	}
	return cmd.run(ctx, c, w, flags, rest)
}

func resolve(cwd, p string) string {
	if strings.HasPrefix(p, "/") {
		return vfs.Clean(p)
	}
	return vfs.Clean(path.Join(cwd, p))
}

// parseFlags extracts single-letter flags (e.g. -r, -rf) from anywhere in args.
func parseFlags(args []string, allowed string) (map[rune]bool, []string, error) {
	flags := map[rune]bool{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			for _, r := range a[1:] {
				if !strings.ContainsRune(allowed, r) {
					return nil, nil, fmt.Errorf("unknown flag -%c", r)
				}
				flags[r] = true
			}
			continue
		}
		rest = append(rest, a)
	}
	return flags, rest, nil
}
