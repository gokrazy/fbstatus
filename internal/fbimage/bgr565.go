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

package fbimage

import (
	"image"
	"image/color"
)

type BGR565 struct {
	Pix    []byte
	Rect   image.Rectangle
	Stride int
}

func (i *BGR565) Bounds() image.Rectangle { return i.Rect }
func (i *BGR565) ColorModel() color.Model { return color.NRGBAModel }

func (i *BGR565) At(x, y int) color.Color {
	if !(image.Point{x, y}.In(i.Rect)) {
		return color.NRGBA{}
	}

	pix := i.Pix[i.PixOffset(x, y):]
	return color.NRGBA{
		R: (pix[1] >> 3) << 3,
		G: (pix[1] << 5) | ((pix[0] >> 5) << 2),
		B: pix[0] << 3,
		A: 255,
	}
}

func (i *BGR565) Set(x, y int, c color.Color) {
	i.SetNRGBA(x, y, color.NRGBAModel.Convert(c).(color.NRGBA))
}

func (i *BGR565) SetNRGBA(x, y int, c color.NRGBA) {
	if !(image.Point{x, y}.In(i.Rect)) {
		return
	}

	pix := i.Pix[i.PixOffset(x, y):]
	pix[0] = (c.B >> 3) | ((c.G >> 2) << 5)
	pix[1] = (c.G >> 5) | ((c.R >> 3) << 3)
}

func (i *BGR565) PixOffset(x, y int) int {
	return (y-i.Rect.Min.Y)*i.Stride + (x-i.Rect.Min.X)*2
}
