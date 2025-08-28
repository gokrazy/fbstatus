// Package console allows working with Linux consoles in graphics mode,
// typically for using the Linux frame buffer.
package console

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"unsafe"

	"github.com/gokrazy/fbstatus/internal/linuxvt"
	"golang.org/x/sys/unix"
)

const tty = "/dev/tty0"

func nextFreeConsole() (int, error) {
	f, err := os.OpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	free, err := unix.IoctlGetInt(int(f.Fd()), linuxvt.VT_OPENQRY)
	if err != nil {
		return 0, fmt.Errorf("VT_OPENQRY: %v", err)
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return free, nil
}

func disallocateConsole(num int) error {
	f, err := os.OpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.IoctlSetInt(int(f.Fd()), linuxvt.VT_DISALLOCATE, num); err != nil {
		return fmt.Errorf("VT_DISALLOCATE(%d): %v", num, err)
	}
	return f.Close()
}

func handleSwitches(fd uintptr, hdl *Handle) error {
	var mode linuxvt.VTMode
	if _, _, eno := unix.Syscall(unix.SYS_IOCTL, fd, linuxvt.VT_GETMODE, uintptr(unsafe.Pointer(&mode))); eno != 0 {
		return fmt.Errorf("VT_GETMODE: %v", eno)
	}

	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, unix.SIGUSR1)
	go func() {
		for range usr1 {
			log.Printf("user switched to different VT, no longer visible")
			hdl.setVisible(false)
			if err := unix.IoctlSetInt(int(fd), linuxvt.VT_RELDISP, 1); err != nil {
				log.Printf("VT_RELDISP: %v", err)
			}
		}
	}()

	usr2 := make(chan os.Signal, 1)
	signal.Notify(usr2, unix.SIGUSR2)
	go func() {
		for range usr2 {
			log.Printf("user switched back, now visible")
			hdl.setVisible(true)
			if err := unix.IoctlSetInt(int(fd), linuxvt.VT_RELDISP, linuxvt.VT_ACKACQ); err != nil {
				log.Printf("VT_RELDISP: %v", err)
			}
			select {
			case hdl.redraw <- struct{}{}:
			default:
			}
		}
	}()

	mode.Mode = linuxvt.VT_PROCESS
	mode.Relsig = int16(unix.SIGUSR1)
	mode.Acqsig = int16(unix.SIGUSR2)

	hdl.setVisible(true)

	if _, _, eno := unix.Syscall(unix.SYS_IOCTL, fd, linuxvt.VT_SETMODE, uintptr(unsafe.Pointer(&mode))); eno != 0 {
		return fmt.Errorf("VT_SETMODE: %v", eno)
	}

	return nil
}

func unhandleSwitches(fd uintptr) error {
	var mode linuxvt.VTMode
	if _, _, eno := unix.Syscall(unix.SYS_IOCTL, fd, linuxvt.VT_GETMODE, uintptr(unsafe.Pointer(&mode))); eno != 0 {
		return fmt.Errorf("VT_GETMODE: %v", eno)
	}

	mode.Mode = linuxvt.VT_AUTO
	mode.Relsig = 0
	mode.Acqsig = 0

	if _, _, eno := unix.Syscall(unix.SYS_IOCTL, fd, linuxvt.VT_SETMODE, uintptr(unsafe.Pointer(&mode))); eno != 0 {
		return fmt.Errorf("VT_SETMODE: %v", eno)
	}

	return nil
}

// A Handle represents an active Linux console.
type Handle struct {
	f      *os.File
	vt     int
	prevVT int
	redraw chan struct{}

	visibleMu sync.Mutex
	visible   bool
}

// LeaseForGraphics opens the next free Linux console in graphics mode. You must
// call Cleanup() when done to switch back to the previous Linux console.
func LeaseForGraphics() (*Handle, error) {
	// Modeled after https://github.com/g0hl1n/psplash/blob/master/psplash-linuxvt.c
	free, err := nextFreeConsole()
	if err != nil {
		return nil, err
	}
	log.Printf("opening next free console /dev/tty%d", free)

	// open next free console
	//
	// psplash opens this file in non-blocking mode, but that does not seem to
	// be necessary?
	f, err := os.OpenFile(fmt.Sprintf("/dev/tty%d", free), os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	var state linuxvt.VTState
	_, _, eno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), linuxvt.VT_GETSTATE, uintptr(unsafe.Pointer(&state)))
	if eno != 0 {
		return nil, fmt.Errorf("VT_GETSTATE: %v", eno)
	}

	if err := unix.IoctlSetInt(int(f.Fd()), linuxvt.VT_ACTIVATE, free); err != nil {
		return nil, fmt.Errorf("VT_ACTIVATE: %v", err)
	}

	if err := unix.IoctlSetInt(int(f.Fd()), linuxvt.VT_WAITACTIVE, free); err != nil {
		return nil, fmt.Errorf("VT_WAITACTIVE: %v", err)
	}

	hdl := &Handle{
		f:      f,
		vt:     free,
		prevVT: int(state.Active),
		redraw: make(chan struct{}, 1),
	}

	// handle console switches by handling signals
	if err := handleSwitches(f.Fd(), hdl); err != nil {
		return nil, err
	}

	// switch console into graphics mode
	if err := unix.IoctlSetInt(int(f.Fd()), linuxvt.KDSETMODE, linuxvt.KD_GRAPHICS); err != nil {
		return nil, fmt.Errorf("KDSETMODE: %v", err)
	}

	return hdl, nil
}

func (h *Handle) setVisible(v bool) {
	h.visibleMu.Lock()
	defer h.visibleMu.Unlock()
	h.visible = v
}

// Visible returns whether this Linux console is currently visible.
func (h *Handle) Visible() bool {
	h.visibleMu.Lock()
	defer h.visibleMu.Unlock()
	return h.visible
}

// Redraw returns a channel that signals a redraw to the frame buffer is
// necessary because the user switched away and then returned to this Linux
// console.
func (h *Handle) Redraw() <-chan struct{} {
	return h.redraw
}

// Cleanup switches the current console from graphics mode back to text mode,
// then switches to the previous console, and finally disallocates the console.
func (h *Handle) Cleanup() error {
	// switch back to text mode
	if err := unix.IoctlSetInt(int(h.f.Fd()), linuxvt.KDSETMODE, linuxvt.KD_TEXT); err != nil {
		return fmt.Errorf("KDSETMODE: %v", err)
	}

	// ignore switches
	if err := unhandleSwitches(h.f.Fd()); err != nil {
		return err
	}

	// switch back to previous console
	if err := unix.IoctlSetInt(int(h.f.Fd()), linuxvt.VT_ACTIVATE, h.prevVT); err != nil {
		return fmt.Errorf("VT_ACTIVATE: %v", err)
	}
	if err := unix.IoctlSetInt(int(h.f.Fd()), linuxvt.VT_WAITACTIVE, h.prevVT); err != nil {
		return fmt.Errorf("VT_WAITACTIVE: %v", err)
	}

	if err := h.f.Close(); err != nil {
		return err
	}

	close(h.redraw)

	return disallocateConsole(h.vt)
}
