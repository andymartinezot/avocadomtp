package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
)

const shellHelp = `Remote:  ls [-l] [PATH] · cd PATH · pwd · info
         pull [-r] REMOTE [LOCAL] · push [-r] LOCAL... [REMOTE_DIR]
         rm [-r] PATH... · mkdir [-p] PATH... · mv SRC DST
Local:   lls [PATH] · lcd PATH · lpwd
Other:   help · exit (or Ctrl-D, or Ctrl-C at the prompt). Ctrl-C cancels a running transfer.
Paths with spaces need quotes: cd "Internal shared storage"`

func (c *client) shell() error {
	fmt.Printf("Connected to %s %s. Type \"help\" for commands.\n", printable(c.r.Device().Manufacturer), printable(c.r.Device().Model))
	cwd := "/"
	if st, err := c.fs.Storages(context.Background()); err == nil && len(st) > 0 {
		cwd = "/" + st[0].Name
	}

	// Ctrl-C cancels the running command; at the prompt it quits.
	var running atomic.Pointer[context.CancelFunc]
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		for range sig {
			if cancel := running.Load(); cancel != nil {
				(*cancel)()
				continue
			}
			fmt.Println()
			c.close()
			os.Exit(130)
		}
	}()

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("avocadomtp:%s> ", cwd)
		if !in.Scan() {
			fmt.Println()
			return in.Err()
		}
		args, err := splitArgs(in.Text())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		if len(args) == 0 {
			continue
		}
		if args[0] == "exit" || args[0] == "quit" {
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		running.Store(&cancel)
		err = c.shellCmd(ctx, &cwd, args)
		running.Store(nil)
		cancel()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				err = errors.New("cancelled")
			}
			fmt.Fprintln(os.Stderr, "error:", printable(err.Error()))
		}
		if herr := c.healthy(); herr != nil {
			return fmt.Errorf("lost connection to the phone: %w", herr)
		}
	}
}

func (c *client) shellCmd(ctx context.Context, cwd *string, args []string) error {
	switch args[0] {
	case "help", "?":
		fmt.Println(shellHelp)
		return nil
	case "pwd":
		fmt.Println(*cwd)
		return nil
	case "cd":
		target := "/"
		if len(args) > 1 {
			target = resolve(*cwd, args[1])
		}
		e, err := c.fs.Stat(ctx, target)
		if err != nil {
			return err
		}
		if !e.IsDir {
			return fmt.Errorf("%s is not a folder", e.Path)
		}
		*cwd = e.Path
		return nil
	case "lpwd":
		wd, err := os.Getwd()
		fmt.Println(wd)
		return err
	case "lcd":
		dir, _ := os.UserHomeDir()
		if len(args) > 1 {
			dir = expandHome(args[1])
		}
		return os.Chdir(dir)
	case "lls":
		dir := "."
		if len(args) > 1 {
			dir = expandHome(args[1])
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if e.IsDir() {
				fmt.Println(e.Name() + "/")
			} else {
				fmt.Println(e.Name())
			}
		}
		return nil
	case "get":
		args[0] = "pull"
	case "put":
		args[0] = "push"
	}
	if args[0] == "push" {
		// "push file" in the shell means "into the current folder".
		_, rest, err := parseFlags(args[1:], "r")
		if err == nil && len(rest) == 1 {
			args = append(args, *cwd)
		}
		for i := 1; i < len(args)-1; i++ {
			args[i] = expandHome(args[i])
		}
	}
	if args[0] == "pull" && len(args) > 2 {
		args[len(args)-1] = expandHome(args[len(args)-1])
	}
	return c.run(ctx, os.Stdout, *cwd, args)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// splitArgs splits a command line honoring single/double quotes and backslash escapes.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quote, esc := false, rune(0), false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 || esc {
		return nil, errors.New("unterminated quote")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}
