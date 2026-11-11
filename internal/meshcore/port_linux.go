// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package meshcore

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// crtscts is the Linux CRTSCTS bit. Hardware flow control on a
// companion cable blocks the open when the radio does not drive CTS.
const crtscts uint32 = 0x80000000

// Open opens a companion radio in raw serial mode. The file must be a
// USB serial device. The returned error is ErrDenied, ErrBusy, or
// ErrDown, and it does not include the path.
func Open(path string) (io.ReadWriteCloser, error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, openErr(err)
	}
	if err := raw(path, int(f.Fd())); err != nil {
		f.Close()
		return nil, openErr(err)
	}
	return f, nil
}

func raw(path string, fd int) error {
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	// Identify the device before changing its line settings. A by-id
	// name is a symlink, and the target must be a USB serial port.
	if err := usbSerial(path, fd); err != nil {
		return err
	}
	// TCGETS2 carries the separate input and output speeds. TCGETS
	// leaves those fields unused, so a ttyUSB adapter stayed at 9600.
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS2)
	if err != nil {
		return err
	}
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	termios.Oflag &^= unix.OPOST
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CBAUD | unix.CBAUDEX | crtscts
	termios.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL | unix.B115200
	termios.Ispeed = unix.B115200
	termios.Ospeed = unix.B115200
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS2, termios); err != nil {
		return err
	}
	// Exclusive so another local process cannot read replies or inject
	// a direct message on this port.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCEXCL), 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func usbSerial(path string, fd int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFCHR {
		return syscall.ENOTTY
	}
	switch unix.Major(st.Rdev) {
	case 166, 188: // ttyACM, ttyUSB
		return nil
	case 136: // pty slave, only when Open is given that path directly
		if ptySlave(path) {
			return nil
		}
	}
	return syscall.ENOTTY
}

func ptySlave(path string) bool {
	rest, ok := strings.CutPrefix(path, "/dev/pts/")
	if !ok || rest == "" {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}

func openErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return ErrDenied
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EBUSY) {
		return ErrBusy
	}
	return ErrDown
}
