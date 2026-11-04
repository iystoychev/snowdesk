//go:build darwin

// Command snow lets it snow on your macOS windows.
package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore -framework Metal
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

	"coke-everywhere/login"
	"coke-everywhere/snow"
)

// Cocoa must run on the main thread.
func init() { runtime.LockOSThread() }

const (
	dotFloats   = C.SNOW_DOT_FLOATS // shared draw buffer: dots, then triangles
	triFloats   = C.SNOW_TRI_FLOATS
	maxWindows  = 256
	windowsEach = 3  // re-read the window list every N frames (~20 Hz)
	screenEach  = 60 // re-read screen size and Dock estimate (~1 Hz); it is costly
)

var (
	sim     *snow.Sim
	cbuf    *C.float  // C memory, so Cocoa may read it after snowStep returns
	dots    []float32 // views into cbuf
	tris    []float32
	wins    [maxWindows]C.SnowWin
	sc      C.SnowScreen
	frame   int
	sinceWL float64
)

func main() {
	C.screenInfo(&sc)
	sim = snow.New(float64(sc.w), float64(sc.h), uint64(time.Now().UnixNano()))
	cbuf = (*C.float)(C.malloc((dotFloats + triFloats) * C.sizeof_float))
	all := unsafe.Slice((*float32)(unsafe.Pointer(cbuf)), dotFloats+triFloats)
	dots, tris = all[:dotFloats], all[dotFloats:]
	refresh(0)
	if home, err := os.UserHomeDir(); err == nil && login.Enabled(home) {
		// Keep start at login pointing at this binary if it was moved.
		if err := setLogin(true); err != nil {
			fmt.Fprintln(os.Stderr, "CokeEverywhere: start at login:", err)
		}
	}
	C.runApp()
}

// refresh feeds window and Dock geometry into the simulation.
func refresh(dt float64) {
	n := int(C.listWindows(&wins[0], maxWindows))
	ws := make([]snow.Window, n)
	onScreen := make(map[uint32]bool, n)
	for i, w := range wins[:n] {
		ws[i] = snow.Window{ID: uint32(w.id), Bounds: snow.Rect{X: float64(w.x), Y: float64(w.y), W: float64(w.w), H: float64(w.h)}}
		onScreen[ws[i].ID] = true
	}
	// Windows with snow that left the screen may only be on another Space or
	// minimized; those keep their pile.
	var gone []C.uint
	for _, id := range sim.PileIDs() {
		if !onScreen[id] {
			gone = append(gone, C.uint(id))
		}
	}
	if len(gone) > 0 {
		exists := make([]C.uchar, len(gone))
		C.windowsExist(&gone[0], C.int(len(gone)), &exists[0])
		for i, id := range gone {
			if exists[i] != 0 {
				ws = append(ws, snow.Window{ID: uint32(id), Hidden: true})
			}
		}
	}
	dock := snow.Rect{X: float64(sc.dockX), Y: float64(sc.dockY), W: float64(sc.dockW), H: float64(sc.dockH)}
	sim.SetWindows(ws, dock, dt)
}

// snowConfigure applies the menu settings. Called on the main thread.
//
//export snowConfigure
func snowConfigure(c C.SnowSettings) {
	sim.Configure(snow.Settings{
		Amount: float64(c.amount), Size: float64(c.size), Piles: c.piles != 0,
		Lights: c.lights != 0, Icicles: c.icicles != 0, Wind: c.wind != 0,
		Sparkles: c.sparkles != 0, Frost: c.frost != 0,
	})
}

// snowTitle returns the menu bar title with the Christmas countdown. The
// caller frees it.
//
//export snowTitle
func snowTitle() *C.char { return C.CString(snow.CountdownTitle(time.Now())) }

// snowStep advances one frame and returns the draw buffer: dots first,
// triangles at SNOW_DOT_FLOATS. The overlay's 60 Hz timer calls it on the
// main thread.
//
//export snowStep
func snowStep(dt C.double, nDots, nTris *C.int) *C.float {
	sinceWL += float64(dt)
	frame++
	if frame%screenEach == 0 {
		C.screenInfo(&sc)
		sim.W, sim.H = float64(sc.w), float64(sc.h)
	}
	// During a desktop slide, track windows every frame so snow and lights
	// slide along smoothly.
	if frame%windowsEach == 0 || sim.Sliding() {
		refresh(sinceWL)
		sinceWL = 0
	}
	var x, y C.double
	C.cursorPos(&x, &y)
	sim.Step(float64(dt), snow.Point{X: float64(x), Y: float64(y)})
	nd, nt := sim.Fill(dots, tris)
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
