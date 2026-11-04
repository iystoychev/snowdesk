// C interface between Go (main.go) and the Cocoa overlay (overlay_darwin.m).
// All rects are in points, top-left origin, main screen coordinates.
#pragma once

typedef struct {
	unsigned int id;
	double x, y, w, h;
} SnowWin;

typedef struct {
	double w, h;                       // main screen size
	double dockX, dockY, dockW, dockH; // estimated Dock rect; dockW == 0 when none
} SnowScreen;

// Shared draw buffer: SNOW_DOT_FLOATS floats of dots (8 each), then
// SNOW_TRI_FLOATS floats of triangle vertices (6 each). See snow/draw.go.
#define SNOW_DOT_FLOATS (1 << 16)
#define SNOW_TRI_FLOATS (1 << 20)

typedef struct {
	double amount, size;
	int piles, lights, icicles, wind, sparkles, frost;
} SnowSettings;

// runApp starts the Cocoa app and never returns. Call on the main thread.
void runApp(void);

// listWindows fills out with normal on-screen windows, front to back,
// excluding our own. Returns how many were written.
int listWindows(SnowWin *out, int max);

// windowsExist sets exists[i] to 1 if window ids[i] still exists anywhere
// (another Space, minimized), else 0.
void windowsExist(const unsigned int *ids, int n, unsigned char *exists);

void screenInfo(SnowScreen *s);

void cursorPos(double *x, double *y);
