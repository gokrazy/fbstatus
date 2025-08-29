// Program fbstatus graphically shows the gokrazy system status on the Linux
// frame buffer, which is typically available via HDMI when running on a
// Raspberry Pi or a PC.
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

	"github.com/fogleman/gg"
	"github.com/gokrazy/fbstatus/internal/console"
	"github.com/gokrazy/fbstatus/internal/fb"
	"github.com/gokrazy/fbstatus/internal/fbimage"
	"github.com/gokrazy/gokrazy"
	"github.com/gokrazy/stat/statexp"
	"github.com/golang/freetype/truetype"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"

	_ "embed"
	_ "image/png"
)

/* Define the layout fractions.
 *
 * There are two columns at the top, and the bottom is just one part.
 * The top left show host information, time and IP addresses.
 * The top right shows the "gokrazy!" tag line and the Gopher below it.
 * The bottom shows the system resource use, refreshed regularly.
 *
 *  -------------------------
 * | host info    | gokrazy! |
 * |              | -------- |
 * | IP addresses | (gopher) |
 * |              |          |
 * |-------------------------|
 * |                         |
 * |                         |
 * |   status information    |
 * |                         |
 * |                         |
 *  -------------------------
 */
const leftFrac = 1.0 / 2.0
const topFrac = 1.0 / 2.0

func uptime() (string, error) {
	file, err := os.Open("/proc/uptime")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := scanner.Text()
		parts := strings.Split(text, " ")
		dur, err := time.ParseDuration(parts[0] + "s")
		if err != nil {
			return "", err
		}
		return dur.Round(time.Second).String(), nil
	}
	return "", fmt.Errorf("BUG: parse /proc/uptime")
}

func scaleImage(bounds image.Rectangle, maxW, maxH int) image.Rectangle {
	imgW := bounds.Max.X
	imgH := bounds.Max.Y
	ratio := float64(maxW) / float64(imgW)
	if r := float64(maxH) / float64(imgH); r < ratio {
		ratio = r
	}
	scaledW := int(ratio * float64(imgW))
	scaledH := int(ratio * float64(imgH))
	return image.Rect(0, 0, scaledW, scaledH)
}

var colorNameToRGBA = map[string]color.NRGBA{
	"darkgray": color.NRGBA{R: 0x55, G: 0x57, B: 0x53},
	"red":      color.NRGBA{R: 0xEF, G: 0x29, B: 0x29},
	"green":    color.NRGBA{R: 0x8A, G: 0xE2, B: 0x34},
	"yellow":   color.NRGBA{R: 0xFC, G: 0xE9, B: 0x4F},
	"blue":     color.NRGBA{R: 0x72, G: 0x9F, B: 0xCF},
	"magenta":  color.NRGBA{R: 0xEE, G: 0x38, B: 0xDA},
	"cyan":     color.NRGBA{R: 0x34, G: 0xE2, B: 0xE2},
	"white":    color.NRGBA{R: 0xEE, G: 0xEE, B: 0xEC},
}

type statusDrawer struct {
	// config
	img         draw.Image
	bounds      image.Rectangle
	w, h        int
	scaleFactor float64
	buffer      *image.RGBA
	files       map[string]*os.File
	bgcolor     color.RGBA
	hostname    string
	modules     []statexp.ProcessAndFormatter
	ghost       *gg.Context
	gstat       *gg.Context
	ggopher     *gg.Context

	// state
	slowPathNotified     bool
	last                 [][][]string
	lastRender, lastCopy time.Duration
}

func newStatusDrawer(img draw.Image) (*statusDrawer, error) {
	bounds := img.Bounds()
	w := bounds.Max.X
	h := bounds.Max.Y

	hostW := int(float64(w) * leftFrac)
	gopherW := w - hostW

	topH := int(float64(h) * topFrac)
	bottomH := h - topH

	scaleFactor := math.Floor(float64(w) / 1024)
	if scaleFactor < 1 {
		scaleFactor = 1
	}
	log.Printf("font scale factor: %.f", scaleFactor)

	// draw the gokrazy gopher image
	gokrazyLogo, _, err := image.Decode(bytes.NewReader(gokrazyLogoPNG))
	if err != nil {
		return nil, err
	}

	bgcolor := color.RGBA{R: 50, G: 50, B: 50, A: 255}

	// We do all rendering into an *image.RGBA buffer, for which all drawing
	// operations are optimized in Go. Only at the very end do we copy the
	// buffer contents to the framebuffer (BGR565 or BGRA)
	buffer := image.NewRGBA(bounds)
	draw.Draw(buffer, bounds, &image.Uniform{bgcolor}, image.Point{}, draw.Src)

	// NOTE: The gopher is drawn exactly once. Other areas are being refreshed.
	// place the gopher in the top right column (centered)
	borderTop := int(50 * scaleFactor)
	gopherRect := scaleImage(gokrazyLogo.Bounds(), gopherW, topH-borderTop)
	// add the left column width
	gopherRect = gopherRect.Add(image.Point{hostW, 0})
	// add the padding between column start and Gopher start for centering
	padX := (gopherW - gopherRect.Size().X) / 2
	padY := borderTop + (topH-gopherRect.Size().Y)/2
	gopherRect = gopherRect.Add(image.Point{padX, padY})

	t1 := time.Now()
	xdraw.BiLinear.Scale(buffer, gopherRect, gokrazyLogo, gokrazyLogo.Bounds(), draw.Over, nil)
	log.Printf("gopher scaled in %v", time.Since(t1))

	ghost := gg.NewContext(hostW, topH)
	ggopher := gg.NewContext(gopherW, topH)
	gstat := gg.NewContext(w, bottomH)

	// draw textual information in a block of key: value details
	font, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}

	size := float64(16)
	size *= scaleFactor
	face := truetype.NewFace(font, &truetype.Options{Size: size})
	ghost.SetFontFace(face)

	monofont, err := truetype.Parse(gomono.TTF)
	if err != nil {
		return nil, err
	}
	monoface := truetype.NewFace(monofont, &truetype.Options{Size: size})
	gstat.SetFontFace(monoface)

	italicfont, err := truetype.Parse(goitalic.TTF)
	if err != nil {
		return nil, err
	}
	italicface := truetype.NewFace(italicfont, &truetype.Options{Size: 2 * size})
	ggopher.SetFontFace(italicface)

	{
		r, gg, b, a := bgcolor.RGBA()
		ggopher.SetRGBA(
			float64(r)/0xffff,
			float64(gg)/0xffff,
			float64(b)/0xffff,
			float64(a)/0xffff)
	}
	ggopher.Clear()
	ggopher.SetRGB(1, 1, 1)
	// padding within the gopher column
	padX = (gopherW - int(66*scaleFactor)) / 2
	ggopher.DrawString("gokrazy!", float64(padX)-(30*scaleFactor), 42*scaleFactor)
	// Only render the tagline once, which is part of the right column.
	// This and the gopher do not need to be redrawn.
	rightCol := image.Rect(hostW, 0, w, int(50*scaleFactor))
	draw.Draw(buffer, rightCol, ggopher.Image(), image.ZP, draw.Src)

	hostname, err := os.Hostname()
	if err != nil {
		log.Print(err)
	}

	// --------------------------------------------------------------------------------
	modules := statexp.DefaultModules()
	files := make(map[string]*os.File)
	for _, mod := range modules {
		// When a stats module implements the FileContents() interface, we
		// ensure all returned file contents are read and passed to
		// ProcessAndFormat.
		fc, ok := mod.(interface{ FileContents() []string })
		if !ok {
			continue
		}
		for _, f := range fc.FileContents() {
			if _, ok := files[f]; ok {
				continue // already requested
			}
			fl, err := os.Open(f)
			if err != nil {
				return nil, err
			}
			files[f] = fl
		}
	}

	// --------------------------------------------------------------------------------

	return &statusDrawer{
		img:         img,
		bounds:      bounds,
		w:           w,
		h:           h,
		scaleFactor: scaleFactor,
		buffer:      buffer,
		modules:     modules,
		hostname:    hostname,
		files:       files,
		bgcolor:     bgcolor,
		ghost:       ghost,
		gstat:       gstat,

		last: make([][][]string, 10),
	}, nil
}

func (d *statusDrawer) draw1(ctx context.Context) error {
	const lineSpacing = 1.5

	// --------------------------------------------------------------------------------
	contents := make(map[string][]byte)
	for path, fl := range d.files {
		if _, err := fl.Seek(0, io.SeekStart); err != nil {
			return err
		}
		b, err := ioutil.ReadAll(fl)
		if err != nil {
			return err
		}
		contents[path] = b
	}

	{
		r, gg, b, a := d.bgcolor.RGBA()
		d.gstat.SetRGBA(
			float64(r)/0xffff,
			float64(gg)/0xffff,
			float64(b)/0xffff,
			float64(a)/0xffff)
	}
	d.gstat.Clear()
	d.gstat.SetRGB(1, 1, 1)

	// use the width only
	em, _ := d.gstat.MeasureString("m")

	// offset from top left corner
	xOffset := 3.0 * em
	yOffset := 3.0 * em
	// extra spacing per additional row/column
	xSpacing := 3.0 * em
	ySpacing := 3.0 * em

	statx := xOffset
	staty := yOffset

	// render header
	// TODO: look into why MeasureString/DrawString are not monospace-correct
	for _, hdr := range []string{
		" usr",
		" sys",
		" idl",
		" wai",
		" stl",
		" | ",
		" read ",
		" writ ",
		" | ",
		" int  ",
		" csw  ",
		" | ",
		" recv ",
		" send ",
		" | ",
		" used ",
		" free ",
		" buff ",
		" cach",
	} {
		d.gstat.DrawString(hdr, statx, staty)
		statx += float64(len(hdr)) * em
	}

	// reset
	statx = xOffset
	// add some more space from the top for actual status lines
	staty = yOffset + ySpacing

	for idx := range d.last {
		if idx == len(d.last)-1 {
			break
		}
		d.last[idx] = d.last[idx+1]
	}

	var lastrow [][]string
	for _, mod := range d.modules {
		var modcols []string
		cols := mod.ProcessAndFormat(contents)
		for _, col := range cols {
			colored := col.RenderCustom(func(color, text string) string {
				return "$" + color + "$" + text
			})
			modcols = append(modcols, colored)
		}
		lastrow = append(lastrow, modcols)
	}
	d.last[len(d.last)-1] = lastrow

	for _, lastrow := range d.last {
		statx = xOffset
		for _, modcols := range lastrow {
			for _, colored := range modcols {
				statx += em
				for idx, field := range strings.Split(strings.TrimPrefix(colored, "$"), "$") {

					if idx%2 == 0 {
						col := colorNameToRGBA[field]
						d.gstat.SetRGB255(int(col.R), int(col.G), int(col.B))
					} else {
						d.gstat.DrawString(field, statx, staty)
						statx += float64(len(field)) * em
					}
				}

			}
			statx += xSpacing
		}
		staty += d.gstat.FontHeight() * lineSpacing
	}

	// --------------------------------------------------------------------------------

	t2 := time.Now()
	{
		r, gg, b, a := d.bgcolor.RGBA()
		d.ghost.SetRGBA(
			float64(r)/0xffff,
			float64(gg)/0xffff,
			float64(b)/0xffff,
			float64(a)/0xffff)
	}
	d.ghost.Clear()
	d.ghost.SetRGB(1, 1, 1)
	lines := []string{
		"host “" + d.hostname + "” (" + gokrazy.Model() + ")",
		"time: " + time.Now().Format(time.RFC3339),
	}
	if up, err := uptime(); err == nil {
		last := len(lines) - 1
		lines[last] += ", up for " + up
	}
	if d.lastRender > 0 || d.lastCopy > 0 {
		last := len(lines) - 1
		lines[last] += fmt.Sprintf(", fb: draw %v, cp %v",
			d.lastRender.Round(time.Millisecond),
			d.lastCopy.Round(time.Millisecond))
	}
	lines = append(lines, "")
	lines = append(lines, "Private IP addresses:")
	if addrs, err := gokrazy.PrivateInterfaceAddrs(); err == nil {
		sort.Strings(addrs)
		for _, addr := range addrs {
			// Filter out loopback addresses (127.0.0.1 and ::1 typically), as
			// they are always present.
			if net.ParseIP(addr).IsLoopback() {
				continue
			}

			lines = append(lines, addr)
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Public IP addresses:")
	if addrs, err := gokrazy.PublicInterfaceAddrs(); err == nil {
		sort.Strings(addrs)
		lines = append(lines, addrs...)
	}
	texty := int(yOffset + ySpacing)

	for _, line := range lines {
		d.ghost.DrawString(line, xOffset, float64(texty))
		texty += int(d.ghost.FontHeight() * lineSpacing)
	}

	// global layout: two columns in the top area, bottom for status
	leftCol := image.Rect(0, 0, int(float64(d.w)*leftFrac), d.h)
	statArea := image.Rect(0, int(float64(d.h)*topFrac), d.w, d.h)

	draw.Draw(d.buffer, leftCol, d.ghost.Image(), image.ZP, draw.Src)
	draw.Draw(d.buffer, statArea, d.gstat.Image(), image.ZP, draw.Src)

	d.lastRender = time.Since(t2)

	t3 := time.Now()
	// NOTE: This code path is NOT using double buffering (which is done
	// using the pan ioctl when using the frame buffer), but in practice
	// updates seem smooth enough, most likely because we are only
	// updating timestamps.
	switch x := d.img.(type) {
	case *fbimage.BGR565:
		copyRGBAtoBGR565(x, d.buffer)
	case *fbimage.BGRA:
		copyRGBAtoBGRA(x, d.buffer)
	default:
		if !d.slowPathNotified {
			log.Printf("framebuffer not using pixel format BGR565, falling back to slow path for img type %T", d.img)
			d.slowPathNotified = true
		}
		draw.Draw(d.img, d.bounds, d.buffer, image.Point{}, draw.Src)
	}
	d.lastCopy = time.Since(t3)
	return nil
}

func fbstatus() error {
	ctx := context.Background()

	// Cancel the context instead of exiting the program:
	ctx, canc := signal.NotifyContext(ctx, os.Interrupt)
	defer canc()

	cons, err := console.LeaseForGraphics()
	if err != nil {
		return err
	}
	defer func() {
		if err := cons.Cleanup(); err != nil {
			log.Print(err)
		}
	}()

	dev, err := fb.Open("/dev/fb0")
	if err != nil {
		return err
	}

	if info, err := dev.VarScreeninfo(); err == nil {
		log.Printf("framebuffer screeninfo: %+v", info)
	}

	img, err := dev.Image()
	if err != nil {
		return err
	}

	drawer, err := newStatusDrawer(img)
	if err != nil {
		return err
	}

	tick := time.Tick(1 * time.Second)
	for {
		if cons.Visible() {
			if err := drawer.draw1(ctx); err != nil {
				return err
			}
		}

		select {
		case <-ctx.Done():
			// return to trigger the deferred cleanup function
			return ctx.Err()

		case <-cons.Redraw():
			break // next iteration

		case <-tick:
			break
		}
	}
}

// copyRGBAtoBGR565 is an inlined version of the hot pixel copying loop for the
// special case of copying from an *image.RGBA to an *fbimage.BGR565.
//
// This specialization brings down copying time to 137ms (from 1.8s!) on the
// Raspberry Pi 4.
func copyRGBAtoBGR565(dst *fbimage.BGR565, src *image.RGBA) {
	bounds := dst.Bounds()
	for y := 0; y < bounds.Max.Y; y++ {
		for x := 0; x < bounds.Max.X; x++ {
			var c color.NRGBA

			i := src.PixOffset(x, y)
			// Small cap improves performance, see https://golang.org/issue/27857
			s := src.Pix[i : i+4 : i+4]
			switch s[3] {
			case 0xff:
				c = color.NRGBA{s[0], s[1], s[2], 0xff}
			case 0:
				c = color.NRGBA{0, 0, 0, 0}
			default:
				r := uint32(s[0])
				r |= r << 8
				g := uint32(s[1])
				g |= g << 8
				b := uint32(s[2])
				b |= b << 8
				a := uint32(s[3])
				a |= a << 8

				// Since Color.RGBA returns an alpha-premultiplied color, we
				// should have r <= a && g <= a && b <= a.
				r = (r * 0xffff) / a
				g = (g * 0xffff) / a
				b = (b * 0xffff) / a
				c = color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
			}

			pix := dst.Pix[dst.PixOffset(x, y):]
			pix[0] = (c.B >> 3) | ((c.G >> 2) << 5)
			pix[1] = (c.G >> 5) | ((c.R >> 3) << 3)
		}
	}
}

// copyRGBAtoBGRA is an inlined version of the hot pixel copying loop for the
// special case of copying from an *image.RGBA to an *fbimage.BGRA.
//
// This specialization brings down copying time to 5ms (from 60-70ms) on an
// amd64 qemu VM with virtio VGA.
func copyRGBAtoBGRA(dst *fbimage.BGRA, src *image.RGBA) {
	for i := 0; i < len(src.Pix); i += 4 {
		s := src.Pix[i : i+4 : i+4]
		d := dst.Pix[i : i+4 : i+4]
		d[0], d[1], d[2], d[3] = s[2], s[1], s[0], s[3]
	}
}

//go:embed "gokrazy.png"
var gokrazyLogoPNG []byte

func main() {
	var cpuprofile = flag.String("cpuprofile", "", "cpu profile")
	var debugListen = flag.String("debug-listen", "", "if non-empty, listen address for debug pprof server")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *debugListen != "" {
		go func() {
			log.Printf("Running debug server on %v ...", *debugListen)
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/debug/pprof", http.StatusFound)
			})
			log.Fatal(http.ListenAndServe(*debugListen, nil))
		}()
	}

	if err := fbstatus(); err != nil {
		log.Fatal(err)
	}
}

// framebuffer implementation survey:
//
// - https://github.com/Merovius/srvfb (native! last active 3 years ago)
// type Device
// func Open(dev string) (*Device, error)
// func (d *Device) Close() error
// func (d *Device) Image() (image.Image, error)
// func (d *Device) VarScreeninfo() (VarScreeninfo, error)
// - https://github.com/gen2brain/framebuffer (native! inactive, last active 8 years ago)
// type Canvas
// func Open(dm *DisplayMode) (c *Canvas, err error)
// func (c *Canvas) Accelerated() bool
// func (c *Canvas) Buffer() []byte
// func (c *Canvas) Clear()
// func (c *Canvas) Close() (err error)
// func (c *Canvas) CurrentMode() (*DisplayMode, error)
// func (c *Canvas) File() *os.File
// func (c *Canvas) FindMode(name string) *DisplayMode
// func (c *Canvas) Image() (draw.Image, error)
// func (c *Canvas) Modes() ([]*DisplayMode, error)
// func (c *Canvas) Palette() (color.Palette, error)
// func (c *Canvas) SetPalette(pal color.Palette) error
// - https://github.com/zenhack/framebuffer-go (cgo, last active 8 years ago)
// - https://github.com/kaey/framebuffer (cgo, last active 8 years ago)
//   - https://github.com/orangecms/go-framebuffer (cgo, 4 commits ahead)
// - https://github.com/gonutz/framebuffer (cgo, raspi specific, last active 5 years ago)
