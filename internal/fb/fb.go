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

// Package fb implements Linux frame buffer interaction via ioctls and mmap.
//
// It has been tested on the Raspberry Pi 4 (vc4drmfb) and on a PC (efifb).
//
// This package is originally based on Axel Wagner’s
// https://pkg.go.dev/github.com/Merovius/srvfb/internal/fb package.
package fb

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"unsafe"

	"github.com/gokrazy/fbstatus/internal/fbimage"
	"golang.org/x/sys/unix"
)

type Device struct {
	fd    uintptr
	mmap  []byte
	finfo FixScreeninfo
}

func Open(dev string) (*Device, error) {
	fd, err := unix.Open(dev, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %v", dev, err)
	}
	if int(uintptr(fd)) != fd {
		unix.Close(fd)
		return nil, errors.New("fd overflows")
	}
	d := &Device{fd: uintptr(fd)}

	_, _, eno := unix.Syscall(unix.SYS_IOCTL, d.fd, FBIOGET_FSCREENINFO, uintptr(unsafe.Pointer(&d.finfo)))
	if eno != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %v", eno)
	}

	d.mmap, err = unix.Mmap(fd, 0, int(d.finfo.Smem_len), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("mmap: %v", err)
	}
	return d, nil
}

func (d *Device) VarScreeninfo() (VarScreeninfo, error) {
	var vinfo VarScreeninfo
	_, _, eno := unix.Syscall(unix.SYS_IOCTL, d.fd, FBIOGET_VSCREENINFO, uintptr(unsafe.Pointer(&vinfo)))
	if eno != 0 {
		return vinfo, fmt.Errorf("FBIOGET_VSCREENINFO: %v", eno)
	}
	return vinfo, nil
}

func (d *Device) Image() (draw.Image, error) {
	vinfo, err := d.VarScreeninfo()
	if err != nil {
		return nil, err
	}

	// TODO: select the correct stride and implementation not only based on bpp,
	// but also on the offsets of the pixels.

	if vinfo.Bits_per_pixel == 32 {
		// The Linux efifb driver typically defaults to 32 bpp.

		virtual := image.Rect(0, 0, int(vinfo.Xres_virtual), int(vinfo.Yres_virtual))
		if virtual.Dx()*virtual.Dy()*4 != len(d.mmap) {
			return nil, errors.New("virtual resolution doesn't match framebuffer size")
		}
		visual := image.Rect(int(vinfo.Xoffset), int(vinfo.Yoffset), int(vinfo.Xres), int(vinfo.Yres))
		if !visual.In(virtual) {
			return nil, errors.New("visual resolution not contained in virtual resolution")
		}
		stride := int(d.finfo.Line_length)

		return &fbimage.BGRA{
			Pix:    d.mmap,
			Stride: stride,
			Rect:   visual,
		}, nil
	} else if vinfo.Bits_per_pixel == 16 {
		// The Raspberry Pi vc4drmfb does not offer 32 bpp, and cannot be
		// reconfigured at runtime.

		// {Xres:3840 Yres:2160 Xres_virtual:3840 Yres_virtual:2160 Xoffset:0 Yoffset:0 Bits_per_pixel:16 Grayscale:0
		// Red:{Offset:11 Length:5 Right:0}
		// Green:{Offset:5 Length:6 Right:0}
		// Blue:{Offset:0 Length:5 Right:0} Transp:{Offset:0 Length:0 Right:0} Nonstd:0 Activate:0 Height:290 Width:520 Accel_flags:1 Pixclock:0 Left_margin:0 Right_margin:0 Upper_margin:0 Lower_margin:0 Hsync_len:0 Vsync_len:0 Sync:0 Vmode:0 Rotate:0 Colorspace:0 Reserved:[0 0 0 0]}

		virtual := image.Rect(0, 0, int(vinfo.Xres_virtual), int(vinfo.Yres_virtual))
		if virtual.Dx()*virtual.Dy()*2 != len(d.mmap) {
			return nil, errors.New("virtual resolution doesn't match framebuffer size")
		}
		visual := image.Rect(int(vinfo.Xoffset), int(vinfo.Yoffset), int(vinfo.Xres), int(vinfo.Yres))
		if !visual.In(virtual) {
			return nil, errors.New("visual resolution not contained in virtual resolution")
		}
		stride := int(d.finfo.Line_length)

		if vinfo.Grayscale == 1 {
			return &image.Gray16{
				Pix:    d.mmap,
				Stride: stride,
				Rect:   visual,
			}, nil
		} else {
			return &fbimage.BGR565{
				Pix:    d.mmap,
				Stride: stride,
				Rect:   visual,
			}, nil
		}
	} else {
		return nil, fmt.Errorf("%d bits per pixel unsupported", vinfo.Bits_per_pixel)
	}
}

func (d *Device) Close() error {
	e1 := unix.Munmap(d.mmap)
	if e2 := unix.Close(int(d.fd)); e2 != nil {
		return e2
	}
	return e1
}
