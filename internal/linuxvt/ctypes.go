//go:build ignore
// +build ignore

// generate with: GOARCH=arm go tool cgo -godefs ctypes.go | gofmt > types_arm.go

// Copyright 2018 Axel Wagner
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package linuxvt

/*
#include <linux/vt.h>
#include <linux/kd.h>
*/
import "C"

type VTState C.struct_vt_stat

type VTMode C.struct_vt_mode

const (
	VT_OPENQRY     = C.VT_OPENQRY
	VT_GETSTATE    = C.VT_GETSTATE
	VT_GETMODE     = C.VT_GETMODE
	VT_SETMODE     = C.VT_SETMODE
	VT_ACTIVATE    = C.VT_ACTIVATE
	VT_WAITACTIVE  = C.VT_WAITACTIVE
	VT_DISALLOCATE = C.VT_DISALLOCATE
	VT_PROCESS     = C.VT_PROCESS
	VT_AUTO        = C.VT_AUTO
	VT_ACKACQ      = C.VT_ACKACQ
	VT_RELDISP     = C.VT_RELDISP
)

const (
	KDSETMODE   = C.KDSETMODE
	KDGETMODE   = C.KDGETMODE
	KD_GRAPHICS = C.KD_GRAPHICS
	KD_TEXT     = C.KD_TEXT
)
