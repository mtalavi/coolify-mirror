package ui

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/mtalavi/coolify-mirror/internal/run"
)

// errNoForeground: the menu cannot get the keyboard (see UseForegroundTTY).
var errNoForeground = errors.New("this terminal does not give the menu the keyboard here - start it directly with:  sudo coolify-mirror")

// UseForegroundTTY makes sure the menu can read the keyboard.
//
// `curl … | sudo sh` with sudo's use_pty (the default on Debian 12 and
// Ubuntu) runs the command in the background process group of a pty that
// sudo creates, and sudo does not pass the keyboard into that pty (its own
// stdin is the pipe). Switching that terminal to raw mode or reading from it
// stops the process (SIGTTOU/SIGTTIN): the menu hung right after the banner.
// The person's real terminal is the controlling terminal of the shell above
// sudo; it is not ours, so it can be switched to raw mode and read freely
// while that shell and sudo wait. Output keeps going through sudo's pty.
// It runs once; later calls return the first result (after a switch, fd 0
// is a terminal whose foreground job is sudo, which must not count again).
func UseForegroundTTY() error {
	ttyOnce.Do(func() { ttyErr = useForegroundTTY() })
	return ttyErr
}

var (
	ttyOnce sync.Once
	ttyErr  error
)

func useForegroundTTY() error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}
	pg, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil || pg == unix.Getpgrp() {
		return nil // the foreground job: nothing to do
	}
	own, _ := os.Readlink("/proc/self/fd/0")
	_, ownTTY := procStat(os.Getpid())
	// Walk up to the first process whose controlling terminal is another one
	// (the shell where `curl … | sudo sh` was typed) and read the keyboard there.
	pid := os.Getppid()
	for depth := 0; depth < 10 && pid > 1; depth++ {
		ppid, ttyNr := procStat(pid)
		if ttyNr != 0 && ttyNr != ownTTY {
			if path := ttyPath(ttyNr); path != "" {
				f, err := os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY, 0)
				if err == nil {
					ok := term.IsTerminal(int(f.Fd())) && unix.Dup3(int(f.Fd()), 0, 0) == nil
					f.Close()
					if ok {
						run.Logf("stdin %s is in the background (process group %d, foreground %d): reading the keyboard from %s (terminal of process %d)", own, unix.Getpgrp(), pg, path, pid)
						return nil
					}
				}
			}
		}
		pid = ppid
	}
	run.Logf("stdin %s is in the background and no other terminal was found", own)
	return errNoForeground
}

// procStat returns the parent and the controlling terminal (tty_nr) of pid.
func procStat(pid int) (ppid, ttyNr int) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0
	}
	// pid (comm) state ppid pgrp session tty_nr …; comm may contain spaces.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, 0
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 5 {
		return 0, 0
	}
	ppid, _ = strconv.Atoi(f[1])
	ttyNr, _ = strconv.Atoi(f[4])
	return ppid, ttyNr
}

// ttyPath finds the device file of a tty_nr (pseudo-terminals and consoles).
func ttyPath(ttyNr int) string {
	major := (ttyNr >> 8) & 0xfff
	minor := (ttyNr & 0xff) | ((ttyNr >> 12) & 0xfff00)
	var path string
	switch {
	case major >= 136 && major <= 143:
		path = fmt.Sprintf("/dev/pts/%d", (major-136)*256+minor)
	case major == 4 && minor < 64:
		path = fmt.Sprintf("/dev/tty%d", minor)
	default:
		return ""
	}
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil || uint64(st.Rdev) != uint64(ttyNr) {
		return ""
	}
	return path
}
