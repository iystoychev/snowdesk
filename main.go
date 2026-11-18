//go:build darwin

// Command snow lets it snow on your macOS windows.
package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore -framework Metal -framework Carbon
#include <stdlib.h>
#include "overlay_darwin.h"
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"snowdesk/login"
	"snowdesk/snow"
)

// Cocoa must run on the main thread.
func init() { runtime.LockOSThread() }

const (
	dotFloats   = C.SNOW_DOT_FLOATS // shared draw buffer: dots, then triangles
	triFloats   = C.SNOW_TRI_FLOATS
	maxWindows  = 256
	maxScreens  = 8
	windowsEach = 3  // re-read the window list every N frames (~20 Hz)
	screenEach  = 60 // re-read screens and the Dock estimate (~1 Hz); it is costly
)

// display is one screen with its own snow. Each runs its own simulation
// in its own coordinates (top-left of that screen is 0,0); flakes do not
// cross between screens. Only the main one (with the Dock and menu bar)
// gets the winter scene and the candles.
type display struct {
	id         uint32
	x, y, w, h float64 // in global points, top-left origin of the main screen
	sim        *snow.Sim
}

var (
	displays []*display
	cfg      = snow.DefaultSettings()
	cbuf     *C.float  // C memory, so Cocoa may read it after snowFrame returns
	dots     []float32 // views into cbuf
	tris     []float32
	wins     [maxWindows]C.SnowWin
	scr      [maxScreens]C.SnowScreen
	frame    int
	sinceWL  float64
	seed     = uint64(time.Now().UnixNano())
)

func main() {
	cbuf = (*C.float)(C.malloc((dotFloats + triFloats) * C.sizeof_float))
	all := unsafe.Slice((*float32)(unsafe.Pointer(cbuf)), dotFloats+triFloats)
	dots, tris = all[:dotFloats], all[dotFloats:]
	syncScreens()
	refresh(0)
	if home, err := os.UserHomeDir(); err == nil && login.Enabled(home) {
		// Keep start at login pointing at this binary if it was moved.
		if err := setLogin(true); err != nil {
			fmt.Fprintln(os.Stderr, "SnowDesk: start at login:", err)
		}
	}
	C.runApp()
}

// mainSim is the simulation of the screen with the Dock.
func mainSim() *snow.Sim { return displays[0].sim }

// syncScreens matches displays to the connected screens: new screens get
// fresh snow, changed ones are resized, unplugged ones are dropped.
func syncScreens() {
	n := int(C.screensInfo(&scr[0], maxScreens))
	if n == 0 {
		return
	}
	old := map[uint32]*display{}
	for _, d := range displays {
		old[d.id] = d
	}
	var next []*display
	for i, sc := range scr[:n] {
		d := old[uint32(sc.id)]
		if d == nil {
			d = &display{id: uint32(sc.id)}
			d.sim = snow.New(float64(sc.w), float64(sc.h), seed+uint64(sc.id))
		}
		d.x, d.y, d.w, d.h = float64(sc.x), float64(sc.y), float64(sc.w), float64(sc.h)
		d.sim.W, d.sim.H = d.w, d.h
		d.sim.Main = i == 0
		d.sim.NoDock = i != 0
		configure(d.sim)
		next = append(next, d)
	}
	displays = next
}

func configure(s *snow.Sim) {
	c := cfg
	if !s.Main {
		c.Candles = false
	}
	s.Configure(c)
}

// refresh feeds window and Dock geometry into each screen's simulation.
func refresh(dt float64) {
	var dockUp C.int
	n := int(C.listWindows(&wins[0], maxWindows, &dockUp))
	for i, d := range displays {
		s := d.sim
		if i == 0 {
			s.DockGone = dockUp == 0
		}
		var ws []snow.Window
		onScreen := map[uint32]bool{}
		for _, w := range wins[:n] {
			b := snow.Rect{X: float64(w.x) - d.x, Y: float64(w.y) - d.y, W: float64(w.w), H: float64(w.h)}
			if b.X+b.W <= 0 || b.X >= d.w || b.Y+b.H <= 0 || b.Y >= d.h {
				continue // not on this screen
			}
			ws = append(ws, snow.Window{ID: uint32(w.id), Bounds: b})
			onScreen[uint32(w.id)] = true
		}
		// Windows with snow that left this screen may only be on another
		// Space, minimized or moved to another screen; those keep their pile.
		var gone []C.uint
		for _, id := range s.PileIDs() {
			if !onScreen[id] {
				gone = append(gone, C.uint(id))
			}
		}
		if len(gone) > 0 {
			exists := make([]C.uchar, len(gone))
			C.windowsExist(&gone[0], C.int(len(gone)), &exists[0])
			for k, id := range gone {
				if exists[k] != 0 {
					ws = append(ws, snow.Window{ID: uint32(id), Hidden: true})
				}
			}
		}
		var dock snow.Rect
		if i == 0 {
			sc := scr[0]
			dock = snow.Rect{X: float64(sc.dockX), Y: float64(sc.dockY), W: float64(sc.dockW), H: float64(sc.dockH)}
		}
		s.SetWindows(ws, dock, dt)
	}
}

// snowConfigure applies the menu settings. Called on the main thread.
//
//export snowConfigure
func snowConfigure(c C.SnowSettings) {
	cfg = snow.Settings{
		Amount: float64(c.amount), Size: float64(c.size), Piles: c.piles != 0,
		Lights: c.lights != 0, Icicles: c.icicles != 0, Wind: c.wind != 0,
		Sparkles: c.sparkles != 0, Frost: c.frost != 0,
		Stars: c.stars != 0, Fireflies: c.fireflies != 0, Leaves: c.leaves != 0,
		Candles: c.candles != 0, CandleCorner: int(c.candleCorner),
		Forest: c.forest != 0, XmasTree: c.xmasTree != 0, Santa: c.santa != 0,
	}
	for _, d := range displays {
		configure(d.sim)
	}
}

// snowCallSanta brings Santa in now.
//
//export snowCallSanta
func snowCallSanta() { mainSim().CallSanta() }

// snowCandleRect sets *r to where the candles rest (top-left origin) and
// returns 1, or returns 0 when candles are off.
//
//export snowCandleRect
func snowCandleRect(r *C.SnowWin) C.int {
	b, ok := mainSim().CandleRect()
	if !ok {
		return 0
	}
	r.x, r.y, r.w, r.h = C.double(b.X), C.double(b.Y), C.double(b.W), C.double(b.H)
	return 1
}

// snowFullscreen reports whether a fullscreen app is showing on the main
// screen; the candle mouse catcher hides then.
//
//export snowFullscreen
func snowFullscreen() C.int {
	if mainSim().Fullscreen() {
		return 1
	}
	return 0
}

// snowCandleDrag moves the candles while the user drags them.
//
//export snowCandleDrag
func snowCandleDrag(dx, dy C.double) { mainSim().DragCandles(float64(dx), float64(dy)) }

// snowCandleDrop ends a drag and returns the corner the candles go to.
//
//export snowCandleDrop
func snowCandleDrop() C.int { return C.int(mainSim().DropCandles()) }

// snowTitle returns the menu bar title with the Christmas countdown. The
// caller frees it.
//
//export snowTitle
func snowTitle() *C.char { return C.CString(snow.CountdownTitle(time.Now())) }

// snowAdvance steps every screen's snow by dt and returns how many screens
// there are. The overlay's 60 Hz timer calls it on the main thread, then
// asks for each screen with snowScreen and snowFrame.
//
//export snowAdvance
func snowAdvance(dt C.double) C.int {
	sinceWL += float64(dt)
	frame++
	if frame%screenEach == 0 {
		syncScreens()
	}
	sliding := false
	for _, d := range displays {
		sliding = sliding || d.sim.Sliding()
	}
	// During a desktop slide, track windows every frame so snow and lights
	// slide along smoothly.
	if frame%windowsEach == 0 || sliding {
		refresh(sinceWL)
		sinceWL = 0
	}
	var x, y C.double
	C.cursorPos(&x, &y)
	opt := C.optionDown() != 0
	for _, d := range displays {
		d.sim.Option = opt
		d.sim.Step(float64(dt), snow.Point{X: float64(x) - d.x, Y: float64(y) - d.y})
	}
	return C.int(len(displays))
}

// snowScreen sets *out to screen i's id and frame (global points).
//
//export snowScreen
func snowScreen(i C.int, out *C.SnowScreen) {
	d := displays[i]
	out.id = C.uint(d.id)
	out.x, out.y, out.w, out.h = C.double(d.x), C.double(d.y), C.double(d.w), C.double(d.h)
}

// snowFrame draws screen i into the shared buffer and returns it: dots
// first, triangles at SNOW_DOT_FLOATS. The caller copies it out before
// asking for the next screen.
//
//export snowFrame
func snowFrame(i C.int, nDots, nTris *C.int) *C.float {
	nd, nt := displays[i].sim.Fill(dots, tris)
	*nDots, *nTris = C.int(nd), C.int(nt)
	return cbuf
}

// setLogin turns start at login on (for this binary) or off.
func setLogin(on bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if !on {
		return login.Disable(home)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	return login.Enable(home, exe)
}

//export snowLoginEnabled
func snowLoginEnabled() C.int {
	home, err := os.UserHomeDir()
	if err == nil && login.Enabled(home) {
		return 1
	}
	return 0
}

// snowSetLogin turns start at login on or off. It returns NULL on success,
// else an error message the caller frees.
//
//export snowSetLogin
func snowSetLogin(on C.int) *C.char {
	if err := setLogin(on != 0); err != nil {
		return C.CString(err.Error())
	}
	return nil
}
