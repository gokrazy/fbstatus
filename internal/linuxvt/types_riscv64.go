// Code generated by cmd/cgo -godefs; DO NOT EDIT.
// cgo -godefs /home/dama/Projects/gokrazy/fbstatus/internal/linuxvt/ctypes.go

package linuxvt

type VTState struct {
	Active uint16
	Signal uint16
	State  uint16
}

type VTMode struct {
	Mode   int8
	Waitv  int8
	Relsig int16
	Acqsig int16
	Frsig  int16
}

const (
	VT_OPENQRY     = 0x5600
	VT_GETSTATE    = 0x5603
	VT_GETMODE     = 0x5601
	VT_SETMODE     = 0x5602
	VT_ACTIVATE    = 0x5606
	VT_WAITACTIVE  = 0x5607
	VT_DISALLOCATE = 0x5608
	VT_PROCESS     = 0x1
	VT_AUTO        = 0x0
	VT_ACKACQ      = 0x2
	VT_RELDISP     = 0x5605
)

const (
	KDSETMODE   = 0x4b3a
	KDGETMODE   = 0x4b3b
	KD_GRAPHICS = 0x1
	KD_TEXT     = 0x0
)
