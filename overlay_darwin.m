// Cocoa side: a click-through transparent overlay that draws what the Go
// simulation produces, plus the window/Dock/cursor queries it needs.
// None of these APIs need Screen Recording or Accessibility permission.
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <Metal/Metal.h>
#import <Carbon/Carbon.h> // RegisterEventHotKey: a global shortcut that needs no permission
#include <unistd.h>
#include "overlay_darwin.h"
#include "_cgo_export.h"

int listWindows(SnowWin *out, int max, int *dockUp) {
	CFArrayRef list = CGWindowListCopyWindowInfo(
		kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
	if (!list) return 0;
	pid_t me = getpid();
	int dockLevel = CGWindowLevelForKey(kCGDockWindowLevelKey);
	*dockUp = 0;
	int n = 0;
	for (CFIndex i = 0; i < CFArrayGetCount(list) && n < max; i++) {
		NSDictionary *w = (__bridge NSDictionary *)CFArrayGetValueAtIndex(list, i);
		int layer = [w[(id)kCGWindowLayer] intValue];
		if (layer == dockLevel && [w[(id)kCGWindowOwnerName] isEqual:@"Dock"]) *dockUp = 1;
		if (layer != 0) continue;
		if ([w[(id)kCGWindowOwnerPID] intValue] == me) continue;
		if ([w[(id)kCGWindowAlpha] doubleValue] < 0.1) continue;
		CGRect r;
		if (!CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)w[(id)kCGWindowBounds], &r)) continue;
		if (r.size.width < 60 || r.size.height < 40) continue;
		out[n++] = (SnowWin){[w[(id)kCGWindowNumber] unsignedIntValue],
		                     r.origin.x, r.origin.y, r.size.width, r.size.height};
	}
	CFRelease(list);
	return n;
}

void windowsExist(const unsigned int *ids, int n, unsigned char *exists) {
	if (n <= 0) return;
	// CGWindowListCreateDescriptionFromArray wants raw CGWindowID values
	// stored as pointers, in an array with no retain callbacks.
	const void **vals = malloc(sizeof(void *) * n);
	for (int i = 0; i < n; i++) {
		exists[i] = 0;
		vals[i] = (const void *)(uintptr_t)ids[i];
	}
	CFArrayRef arr = CFArrayCreate(NULL, vals, n, NULL);
	free(vals);
	CFArrayRef desc = CGWindowListCreateDescriptionFromArray(arr);
	CFRelease(arr);
	if (!desc) return;
	for (CFIndex k = 0; k < CFArrayGetCount(desc); k++) {
		NSDictionary *w = (__bridge NSDictionary *)CFArrayGetValueAtIndex(desc, k);
		unsigned int wid = [w[(id)kCGWindowNumber] unsignedIntValue];
		for (int i = 0; i < n; i++)
			if (ids[i] == wid) exists[i] = 1;
	}
	CFRelease(desc);
}

static NSInteger dockArrayCount(CFStringRef key) {
	CFPropertyListRef v = CFPreferencesCopyAppValue(key, CFSTR("com.apple.dock"));
	if (!v) return 0;
	NSInteger n = CFGetTypeID(v) == CFArrayGetTypeID() ? CFArrayGetCount(v) : 0;
	CFRelease(v);
	return n;
}

static BOOL dockBool(CFStringRef key, BOOL def) {
	Boolean ok = false;
	Boolean v = CFPreferencesGetAppBooleanValue(key, CFSTR("com.apple.dock"), &ok);
	return ok ? v : def;
}

// The Dock's own window spans the whole screen and its icon frames need
// Accessibility, so its width is estimated from its item count instead.
static CGRect estimateDock(NSScreen *s) {
	double inset = NSMinY(s.visibleFrame) - NSMinY(s.frame);
	if (inset < 20) return CGRectZero; // hidden, or on the left/right side

	NSMutableSet *pinned = [NSMutableSet setWithObject:@"com.apple.finder"];
	CFPropertyListRef apps = CFPreferencesCopyAppValue(CFSTR("persistent-apps"), CFSTR("com.apple.dock"));
	if (apps) {
		if (CFGetTypeID(apps) == CFArrayGetTypeID()) {
			for (NSDictionary *a in (__bridge NSArray *)apps) {
				id bid = a[@"tile-data"][@"bundle-identifier"];
				if ([bid isKindOfClass:NSString.class]) [pinned addObject:bid];
			}
		}
		CFRelease(apps);
	}
	NSInteger items = pinned.count;
	for (NSRunningApplication *app in NSWorkspace.sharedWorkspace.runningApplications) {
		if (app.activationPolicy == NSApplicationActivationPolicyRegular && app.bundleIdentifier &&
		    ![pinned containsObject:app.bundleIdentifier]) items++;
	}
	NSInteger seps = 1;
	if (dockBool(CFSTR("show-recents"), YES)) {
		NSInteger r = dockArrayCount(CFSTR("recent-apps"));
		if (r > 0) { items += r; seps++; }
	}
	items += dockArrayCount(CFSTR("persistent-others")) + 1; // + Trash

	// Calibrated on macOS 27: inset 64 → 44pt icons, 20 items → ~938pt wide,
	// Dock top at the edge of the visible frame.
	double tile = inset - 20;
	double W = s.frame.size.width;
	double w = MIN(items * (tile + 1.5) + seps * 12 + 16, W);
	return CGRectMake((W - w) / 2, s.frame.size.height - inset, w, inset);
}

static unsigned int screenID(NSScreen *s) {
	return [s.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
}

int screensInfo(SnowScreen *out, int max) {
	NSArray<NSScreen *> *all = NSScreen.screens;
	if (all.count == 0) return 0;
	double mainH = all.firstObject.frame.size.height;
	int n = 0;
	for (NSScreen *s in all) {
		if (n >= max) break;
		NSRect f = s.frame;
		CGRect d = n == 0 ? estimateDock(s) : CGRectZero;
		out[n++] = (SnowScreen){screenID(s), f.origin.x, mainH - NSMaxY(f), f.size.width, f.size.height,
		                        d.origin.x, d.origin.y, d.size.width, d.size.height};
	}
	return n;
}

void cursorPos(double *x, double *y) {
	NSPoint p = NSEvent.mouseLocation;
	*x = p.x;
	*y = NSScreen.screens.firstObject.frame.size.height - p.y;
}

int optionDown(void) {
	return (NSEvent.modifierFlags & NSEventModifierFlagOption) != 0;
}

// Everything is drawn on the GPU with Metal into one surface. Earlier
// CAShapeLayer/sprite-layer versions made WindowServer rasterize or composite
// thousands of shapes per frame on the CPU, which stalled video playback.
static NSString *const kShaders = @
	"#include <metal_stdlib>\n"
	"using namespace metal;\n"
	"struct U { float2 size; float opacity; };\n"
	"struct VO { float4 pos [[position]]; float2 uv; float inner; float4 col; };\n"
	"static float4 ndc(float2 p, float2 s) { return float4(p.x / s.x * 2 - 1, 1 - p.y / s.y * 2, 0, 1); }\n"
	// Dots: 8 floats each (x, y, radius, inner, r, g, b, a), one quad per instance.
	"vertex VO dotV(uint vid [[vertex_id]], uint iid [[instance_id]],\n"
	"               const device float *f [[buffer(0)]], constant U &u [[buffer(1)]]) {\n"
	"  const float2 c[6] = {{-1,-1},{1,-1},{-1,1},{1,-1},{1,1},{-1,1}};\n"
	"  const device float *d = f + iid * 8;\n"
	"  VO o; o.uv = c[vid]; o.inner = d[3]; o.col = float4(d[4], d[5], d[6], d[7]);\n"
	"  o.pos = ndc(float2(d[0], d[1]) + c[vid] * d[2], u.size); return o;\n"
	"}\n"
	"fragment float4 dotF(VO in [[stage_in]], constant U &u [[buffer(0)]]) {\n"
	"  float a = (1 - smoothstep(in.inner, 1.0, length(in.uv))) * in.col.a * u.opacity;\n"
	"  return float4(in.col.rgb * a, a);\n"
	"}\n"
	// Triangles: 6 floats per vertex (x, y, r, g, b, a).
	"vertex VO triV(uint vid [[vertex_id]], const device float *v [[buffer(0)]], constant U &u [[buffer(1)]]) {\n"
	"  const device float *p = v + vid * 6;\n"
	"  VO o; o.uv = 0; o.inner = 0; o.col = float4(p[2], p[3], p[4], p[5]);\n"
	"  o.pos = ndc(float2(p[0], p[1]), u.size); return o;\n"
	"}\n"
	"fragment float4 triF(VO in [[stage_in]], constant U &u [[buffer(0)]]) {\n"
	"  float a = in.col.a * u.opacity; return float4(in.col.rgb * a, a);\n"
	"}\n";

typedef struct {
	float w, h, opacity, pad;
} Uniforms;

enum { kInFlight = 3 };

static const NSWindowCollectionBehavior kBehavior =
	NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary |
	NSWindowCollectionBehaviorFullScreenAuxiliary | NSWindowCollectionBehaviorIgnoresCycle;

// Overlay is the transparent, click-through window over one screen, with
// its own Metal surface and buffers. Metal drawing happens on its own
// queue, off the main thread.
@interface Overlay : NSObject
@property(readonly) NSWindow *win;
@property unsigned int displayID;
@end

@implementation Overlay {
	CAMetalLayer *_layer;
	id<MTLDevice> _device;
	id<MTLCommandQueue> _queue;
	id<MTLRenderPipelineState> _dotPipe, _triPipe;
	id<MTLBuffer> _dotBuf[kInFlight], _triBuf[kInFlight];
	dispatch_semaphore_t _inflight;
	dispatch_queue_t _renderQ;
	int _slot;
	BOOL _wasEmpty;
}

- (instancetype)initWithScreen:(NSScreen *)s device:(id<MTLDevice>)dev queue:(id<MTLCommandQueue>)q
                       dotPipe:(id<MTLRenderPipelineState>)dp triPipe:(id<MTLRenderPipelineState>)tp {
	if (!(self = [super init])) return nil;
	_device = dev, _queue = q, _dotPipe = dp, _triPipe = tp;
	_displayID = screenID(s);
	_win = [[NSWindow alloc] initWithContentRect:s.frame
	                                   styleMask:NSWindowStyleMaskBorderless
	                                     backing:NSBackingStoreBuffered
	                                       defer:NO];
	_win.opaque = NO;
	_win.backgroundColor = NSColor.clearColor;
	_win.hasShadow = NO;
	_win.ignoresMouseEvents = YES;
	_win.level = NSStatusWindowLevel;
	_win.collectionBehavior = kBehavior;
	_win.releasedWhenClosed = NO;
	NSView *v = _win.contentView;
	v.wantsLayer = YES;
	_layer = [CAMetalLayer layer];
	_layer.device = _device;
	_layer.pixelFormat = MTLPixelFormatBGRA8Unorm;
	_layer.opaque = NO;
	_layer.framebufferOnly = YES;
	[v.layer addSublayer:_layer];
	for (int i = 0; i < kInFlight; i++) {
		_dotBuf[i] = [_device newBufferWithLength:sizeof(float) * SNOW_DOT_FLOATS options:MTLResourceStorageModeShared];
		_triBuf[i] = [_device newBufferWithLength:sizeof(float) * SNOW_TRI_FLOATS options:MTLResourceStorageModeShared];
	}
	_inflight = dispatch_semaphore_create(kInFlight);
	_renderQ = dispatch_queue_create("SnowDesk.render", DISPATCH_QUEUE_SERIAL);
	[self fitScreen:s];
	return self;
}

// fitScreen keeps the window and surface the size of screen s.
- (void)fitScreen:(NSScreen *)s {
	if (!NSEqualRects(_win.frame, s.frame)) [_win setFrame:s.frame display:NO];
	CGFloat scale = s.backingScaleFactor;
	NSRect b = _win.contentView.bounds;
	CGSize px = CGSizeMake(b.size.width * scale, b.size.height * scale);
	if (!CGSizeEqualToSize(_layer.drawableSize, px) || _layer.contentsScale != scale) {
		[CATransaction begin];
		[CATransaction setDisableActions:YES];
		_layer.frame = b;
		_layer.contentsScale = scale;
		_layer.drawableSize = px;
		[CATransaction commit];
	}
}

// draw copies one frame from b (dots, then triangles at SNOW_DOT_FLOATS)
// and renders it. b may be reused as soon as this returns.
- (void)draw:(const float *)b dots:(int)nd tris:(int)nt opacity:(float)opacity {
	BOOL empty = nd == 0 && nt == 0;
	if (empty && _wasEmpty) return; // nothing to draw, surface already clear
	// Never block the main thread on the GPU: skip the frame if it is behind.
	if (dispatch_semaphore_wait(_inflight, DISPATCH_TIME_NOW) != 0) return;
	_wasEmpty = empty;
	_slot = (_slot + 1) % kInFlight;
	int slot = _slot;
	memcpy(_dotBuf[slot].contents, b, sizeof(float) * nd);
	memcpy(_triBuf[slot].contents, b + SNOW_DOT_FLOATS, sizeof(float) * nt);
	CGSize sz = _win.contentView.bounds.size; // points, top-left origin in the shader
	Uniforms u = {sz.width, sz.height, opacity, 0};
	// Waiting for a drawable can block for most of a second while macOS
	// switches desktops; do it off the main thread so the app never freezes.
	dispatch_async(_renderQ, ^{
		[self renderSlot:slot dots:nd tris:nt uniforms:u];
	});
}

// renderSlot draws one frame from buffer slot on the render queue.
- (void)renderSlot:(int)slot dots:(int)nd tris:(int)nt uniforms:(Uniforms)u {
	id<CAMetalDrawable> drawable = [_layer nextDrawable];
	if (!drawable) {
		dispatch_semaphore_signal(_inflight);
		return;
	}
	MTLRenderPassDescriptor *rp = [MTLRenderPassDescriptor renderPassDescriptor];
	rp.colorAttachments[0].texture = drawable.texture;
	rp.colorAttachments[0].loadAction = MTLLoadActionClear;
	rp.colorAttachments[0].clearColor = MTLClearColorMake(0, 0, 0, 0);
	rp.colorAttachments[0].storeAction = MTLStoreActionStore;

	id<MTLCommandBuffer> cb = [_queue commandBuffer];
	id<MTLRenderCommandEncoder> enc = [cb renderCommandEncoderWithDescriptor:rp];
	[enc setVertexBytes:&u length:sizeof u atIndex:1];
	[enc setFragmentBytes:&u length:sizeof u atIndex:0];
	if (nt > 0) { // triangles first: dots (bulbs, flakes) sit on top
		[enc setRenderPipelineState:_triPipe];
		[enc setVertexBuffer:_triBuf[slot] offset:0 atIndex:0];
		[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:nt / 6];
	}
	if (nd > 0) {
		[enc setRenderPipelineState:_dotPipe];
		[enc setVertexBuffer:_dotBuf[slot] offset:0 atIndex:0];
		[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:nd / 8];
	}
	[enc endEncoding];
	dispatch_semaphore_t sem = _inflight;
	[cb addCompletedHandler:^(id<MTLCommandBuffer> _) {
		dispatch_semaphore_signal(sem);
	}];
	[cb presentDrawable:drawable];
	[cb commit];
}
@end

// CandleView catches the mouse over the candles (the overlay ignores it)
// and turns drags into snowCandleDrag / snowCandleDrop calls. AppKit keeps
// sending a drag to the window it started in, so the window can stay put.
@interface CandleView : NSView
@property(copy) void (^onDrop)(int corner);
@end

@implementation CandleView {
	NSPoint _last;
}
- (void)drawRect:(NSRect)r {
	// Fully clear pixels let clicks through; this is invisible but catches them.
	[[NSColor colorWithWhite:0 alpha:0.01] setFill];
	NSRectFill(self.bounds);
}
- (BOOL)acceptsFirstMouse:(NSEvent *)e { return YES; }
- (void)resetCursorRects { [self addCursorRect:self.bounds cursor:NSCursor.openHandCursor]; }
- (void)mouseDown:(NSEvent *)e {
	_last = NSEvent.mouseLocation;
	[NSCursor.closedHandCursor push];
}
- (void)mouseDragged:(NSEvent *)e {
	NSPoint p = NSEvent.mouseLocation;
	snowCandleDrag(p.x - _last.x, _last.y - p.y); // flip y: the sim's origin is top-left
	_last = p;
}
- (void)mouseUp:(NSEvent *)e {
	[NSCursor pop];
	int corner = snowCandleDrop();
	if (self.onDrop) self.onDrop(corner);
}
@end

@interface SnowController : NSObject <NSApplicationDelegate>
- (void)popUpMenu;
@end

@implementation SnowController {
	NSMutableDictionary<NSNumber *, Overlay *> *_overlays; // by display ID
	id<MTLDevice> _device;
	id<MTLCommandQueue> _queue;
	id<MTLRenderPipelineState> _dotPipe, _triPipe;
	float _opacity;
	NSTimer *_timer, *_titleTimer;
	CFTimeInterval _last;
	NSStatusItem *_status;
	NSMenu *_menu;
	EventHotKeyRef _hotKey;
	NSPanel *_candles; // mouse catcher over the candles
	BOOL _full;        // a fullscreen app is showing
}

- (void)start {
	_overlays = [NSMutableDictionary new];
	[self setupMetal];

	NSUserDefaults *d = NSUserDefaults.standardUserDefaults;
	[d registerDefaults:@{
		@"amount" : @1.0, @"size" : @1.0, @"opacity" : @1.0, @"piles" : @YES,
		@"lights" : @YES, @"icicles" : @YES, @"wind" : @YES, @"sparkles" : @YES, @"frost" : @YES,
		@"stars" : @NO, @"fireflies" : @NO, @"leaves" : @NO,
		@"candles" : @NO, @"candleCorner" : @0,
		@"forest" : @YES, @"xmasTree" : @YES, @"santa" : @YES
	}];

	_status = [NSStatusBar.systemStatusBar statusItemWithLength:NSVariableStatusItemLength];
	[self updateTitle];
	// The countdown changes once a day; checking each minute catches midnight.
	_titleTimer = [NSTimer scheduledTimerWithTimeInterval:60 target:self selector:@selector(updateTitle) userInfo:nil repeats:YES];
	NSMenu *m = [NSMenu new];
	NSMenuItem *about = [m addItemWithTitle:@"About SnowDesk" action:@selector(showAbout:) keyEquivalent:@""];
	about.target = self;
	NSMenuItem *repo = [m addItemWithTitle:@"SnowDesk on GitHub…" action:@selector(openRepo:) keyEquivalent:@""];
	repo.target = self;
	[m addItem:NSMenuItem.separatorItem];
	[m addItem:[self choices:@"Snow Amount" key:@"amount"
	                  labels:@[ @"Light", @"Normal", @"Heavy", @"Blizzard" ]
	                  values:@[ @0.4, @1.0, @2.5, @5.0 ]]];
	[m addItem:[self choices:@"Flake Size" key:@"size"
	                  labels:@[ @"Small", @"Normal", @"Large" ]
	                  values:@[ @0.7, @1.0, @1.6 ]]];
	[m addItem:[self choices:@"Opacity" key:@"opacity"
	                  labels:@[ @"40%", @"70%", @"100%" ]
	                  values:@[ @0.4, @0.7, @1.0 ]]];
	[m addItem:[self toggle:@"Pile Up on Windows" key:@"piles"]];
	NSMenuItem *decor = [[NSMenuItem alloc] initWithTitle:@"Decorations" action:nil keyEquivalent:@""];
	decor.submenu = [[NSMenu alloc] initWithTitle:@"Decorations"];
	[decor.submenu addItem:[self toggle:@"Christmas Lights" key:@"lights"]];
	[decor.submenu addItem:[self toggle:@"Icicles" key:@"icicles"]];
	[decor.submenu addItem:[self toggle:@"Wind Gusts" key:@"wind"]];
	[decor.submenu addItem:[self toggle:@"Cursor Sparkles" key:@"sparkles"]];
	[decor.submenu addItem:[self toggle:@"Frosted Corners" key:@"frost"]];
	[decor.submenu addItem:NSMenuItem.separatorItem];
	[decor.submenu addItem:[self toggle:@"Shooting Stars" key:@"stars"]];
	[decor.submenu addItem:[self toggle:@"Fireflies" key:@"fireflies"]];
	[decor.submenu addItem:[self toggle:@"Falling Leaves" key:@"leaves"]];
	[decor.submenu addItem:[self toggle:@"Candles (drag to any corner)" key:@"candles"]];
	[m addItem:decor];
	NSMenuItem *scene = [[NSMenuItem alloc] initWithTitle:@"Winter Scene" action:nil keyEquivalent:@""];
	scene.submenu = [[NSMenu alloc] initWithTitle:@"Winter Scene"];
	[scene.submenu addItem:[self toggle:@"Evergreen Forest" key:@"forest"]];
	[scene.submenu addItem:[self toggle:@"Christmas Tree" key:@"xmasTree"]];
	[scene.submenu addItem:[self toggle:@"Santa Visits" key:@"santa"]];
	[m addItem:scene];
	NSMenuItem *callSanta = [m addItemWithTitle:@"Call Santa Now" action:@selector(callSanta:) keyEquivalent:@""];
	callSanta.target = self;
	NSMenuItem *tip = [m addItemWithTitle:@"Hold ⌥ and sweep a drift to roll a snowball" action:nil keyEquivalent:@""];
	tip.enabled = NO;
	NSMenuItem *tip2 = [m addItemWithTitle:@"Open this menu anywhere: ⌃⌥⌘S" action:nil keyEquivalent:@""];
	tip2.enabled = NO;
	[m addItem:NSMenuItem.separatorItem];
	NSMenuItem *loginItem = [m addItemWithTitle:@"Start at Login" action:@selector(toggleLogin:) keyEquivalent:@""];
	loginItem.target = self;
	loginItem.state = snowLoginEnabled() ? NSControlStateValueOn : NSControlStateValueOff;
	NSMenuItem *pause = [m addItemWithTitle:@"Pause" action:@selector(togglePause:) keyEquivalent:@"p"];
	pause.target = self;
	[m addItem:NSMenuItem.separatorItem];
	[m addItemWithTitle:@"Quit SnowDesk" action:@selector(terminate:) keyEquivalent:@"q"];
	_status.menu = m;
	_menu = m;
	[self registerHotKey];

	[self applySettings];
	[self resume];
}

// choices builds a submenu of radio items that store a number under key.
- (NSMenuItem *)choices:(NSString *)title key:(NSString *)key labels:(NSArray *)labels values:(NSArray<NSNumber *> *)values {
	NSMenuItem *root = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:@""];
	NSMenu *sub = [[NSMenu alloc] initWithTitle:title];
	double cur = [NSUserDefaults.standardUserDefaults doubleForKey:key];
	for (NSUInteger i = 0; i < labels.count; i++) {
		NSMenuItem *it = [sub addItemWithTitle:labels[i] action:@selector(pick:) keyEquivalent:@""];
		it.target = self;
		it.representedObject = @[ key, values[i] ];
		it.state = fabs(cur - values[i].doubleValue) < 1e-6 ? NSControlStateValueOn : NSControlStateValueOff;
	}
	root.submenu = sub;
	return root;
}

- (void)pick:(NSMenuItem *)item {
	NSArray *kv = item.representedObject;
	[NSUserDefaults.standardUserDefaults setDouble:[kv[1] doubleValue] forKey:kv[0]];
	for (NSMenuItem *it in item.menu.itemArray)
		it.state = it == item ? NSControlStateValueOn : NSControlStateValueOff;
	[self applySettings];
}

// toggle builds a checkbox item that flips a saved bool under key.
- (NSMenuItem *)toggle:(NSString *)title key:(NSString *)key {
	NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title action:@selector(flip:) keyEquivalent:@""];
	it.target = self;
	it.representedObject = key;
	it.state = [NSUserDefaults.standardUserDefaults boolForKey:key] ? NSControlStateValueOn : NSControlStateValueOff;
	return it;
}

- (void)flip:(NSMenuItem *)item {
	NSUserDefaults *d = NSUserDefaults.standardUserDefaults;
	BOOL on = ![d boolForKey:item.representedObject];
	[d setBool:on forKey:item.representedObject];
	item.state = on ? NSControlStateValueOn : NSControlStateValueOff;
	[self applySettings];
}

static NSString *const kRepoURL = @"https://github.com/iystoychev/snowdesk";

- (void)showAbout:(id)sender {
	NSMutableAttributedString *credits = [[NSMutableAttributedString alloc]
		initWithString:@"Snow for your Mac.\n"
		    attributes:@{NSFontAttributeName : [NSFont systemFontOfSize:11]}];
	[credits appendAttributedString:[[NSAttributedString alloc]
		initWithString:kRepoURL
		    attributes:@{NSLinkAttributeName : [NSURL URLWithString:kRepoURL], NSFontAttributeName : [NSFont systemFontOfSize:11]}]];
	NSMutableParagraphStyle *center = [NSMutableParagraphStyle new];
	center.alignment = NSTextAlignmentCenter;
	[credits addAttribute:NSParagraphStyleAttributeName value:center range:NSMakeRange(0, credits.length)];
	[NSApp activateIgnoringOtherApps:YES]; // a menu bar app is not active by default
	[NSApp orderFrontStandardAboutPanelWithOptions:@{
		NSAboutPanelOptionApplicationName : @"SnowDesk",
		NSAboutPanelOptionCredits : credits,
	}];
}

// On a Mac with a notch, macOS hides menu bar icons that do not fit, so
// the menu also opens with ⌃⌥⌘S, or by opening SnowDesk again.
static OSStatus hotKeyPressed(EventHandlerCallRef next, EventRef e, void *me) {
	[(__bridge SnowController *)me popUpMenu];
	return noErr;
}

- (void)registerHotKey {
	EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
	InstallApplicationEventHandler(hotKeyPressed, 1, &spec, (__bridge void *)self, NULL);
	EventHotKeyID hid = {'SNOW', 1};
	RegisterEventHotKey(kVK_ANSI_S, cmdKey | optionKey | controlKey, hid, GetApplicationEventTarget(), 0, &_hotKey);
}

- (void)popUpMenu {
	[NSApp activateIgnoringOtherApps:YES];
	[_menu popUpMenuPositioningItem:nil atLocation:NSEvent.mouseLocation inView:nil];
}

- (BOOL)applicationShouldHandleReopen:(NSApplication *)app hasVisibleWindows:(BOOL)flag {
	dispatch_async(dispatch_get_main_queue(), ^{
		[self popUpMenu];
	});
	return NO;
}

- (void)callSanta:(id)sender {
	snowCallSanta();
}

- (void)openRepo:(id)sender {
	[NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:kRepoURL]];
}

- (void)toggleLogin:(NSMenuItem *)item {
	BOOL on = item.state != NSControlStateValueOn;
	char *err = snowSetLogin(on);
	if (err) {
		NSAlert *a = [NSAlert new];
		a.messageText = @"Could not change Start at Login";
		a.informativeText = [NSString stringWithUTF8String:err];
		free(err);
		[NSApp activateIgnoringOtherApps:YES];
		[a runModal];
		return;
	}
	item.state = on ? NSControlStateValueOn : NSControlStateValueOff;
}

- (void)applySettings {
	NSUserDefaults *d = NSUserDefaults.standardUserDefaults;
	snowConfigure((SnowSettings){
		.amount = [d doubleForKey:@"amount"],
		.size = [d doubleForKey:@"size"],
		.piles = [d boolForKey:@"piles"],
		.lights = [d boolForKey:@"lights"],
		.icicles = [d boolForKey:@"icicles"],
		.wind = [d boolForKey:@"wind"],
		.sparkles = [d boolForKey:@"sparkles"],
		.frost = [d boolForKey:@"frost"],
		.stars = [d boolForKey:@"stars"],
		.fireflies = [d boolForKey:@"fireflies"],
		.leaves = [d boolForKey:@"leaves"],
		.candles = [d boolForKey:@"candles"],
		.candleCorner = (int)[d integerForKey:@"candleCorner"],
		.forest = [d boolForKey:@"forest"],
		.xmasTree = [d boolForKey:@"xmasTree"],
		.santa = [d boolForKey:@"santa"],
	});
	[self placeCandles];
	_opacity = [d doubleForKey:@"opacity"];
}

// placeCandles puts the mouse catcher where the candles rest, or hides it.
- (void)placeCandles {
	SnowWin r;
	if (!_timer || !snowCandleRect(&r)) {
		[_candles orderOut:nil];
		return;
	}
	if (!_candles) {
		_candles = [[NSPanel alloc] initWithContentRect:NSZeroRect
		                                      styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
		                                        backing:NSBackingStoreBuffered
		                                          defer:NO];
		_candles.opaque = NO;
		_candles.backgroundColor = NSColor.clearColor;
		_candles.hasShadow = NO;
		_candles.level = NSStatusWindowLevel;
		_candles.collectionBehavior = kBehavior;
		_candles.becomesKeyOnlyIfNeeded = YES;
		CandleView *v = [CandleView new];
		__weak SnowController *me = self;
		v.onDrop = ^(int corner) {
			[NSUserDefaults.standardUserDefaults setInteger:corner forKey:@"candleCorner"];
			[me placeCandles];
		};
		_candles.contentView = v;
	}
	CGFloat h = NSScreen.screens.firstObject.frame.size.height;
	[_candles setFrame:NSMakeRect(r.x, h - r.y - r.h, r.w, r.h) display:YES];
	[_candles.contentView.window invalidateCursorRectsForView:_candles.contentView];
	[_candles orderFrontRegardless];
}

- (void)updateTitle {
	char *t = snowTitle();
	_status.button.title = [NSString stringWithUTF8String:t];
	free(t);
}

- (void)setupMetal {
	_device = MTLCreateSystemDefaultDevice();
	if (!_device) {
		NSLog(@"SnowDesk: no Metal device");
		exit(1);
	}
	_queue = [_device newCommandQueue];

	NSError *err = nil;
	id<MTLLibrary> lib = [_device newLibraryWithSource:kShaders options:nil error:&err];
	if (!lib) {
		NSLog(@"SnowDesk: shader compile failed: %@", err);
		exit(1);
	}
	_dotPipe = [self pipeline:lib vertex:@"dotV" fragment:@"dotF"];
	_triPipe = [self pipeline:lib vertex:@"triV" fragment:@"triF"];
}

- (id<MTLRenderPipelineState>)pipeline:(id<MTLLibrary>)lib vertex:(NSString *)vf fragment:(NSString *)ff {
	MTLRenderPipelineDescriptor *d = [MTLRenderPipelineDescriptor new];
	d.vertexFunction = [lib newFunctionWithName:vf];
	d.fragmentFunction = [lib newFunctionWithName:ff];
	MTLRenderPipelineColorAttachmentDescriptor *c = d.colorAttachments[0];
	c.pixelFormat = MTLPixelFormatBGRA8Unorm;
	c.blendingEnabled = YES; // premultiplied alpha
	c.sourceRGBBlendFactor = c.sourceAlphaBlendFactor = MTLBlendFactorOne;
	c.destinationRGBBlendFactor = c.destinationAlphaBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
	NSError *err = nil;
	id<MTLRenderPipelineState> p = [_device newRenderPipelineStateWithDescriptor:d error:&err];
	if (!p) {
		NSLog(@"SnowDesk: pipeline %@ failed: %@", vf, err);
		exit(1);
	}
	return p;
}

- (void)resume {
	_last = CACurrentMediaTime();
	_timer = [NSTimer timerWithTimeInterval:1.0 / 60 target:self selector:@selector(tick:) userInfo:nil repeats:YES];
	[NSRunLoop.mainRunLoop addTimer:_timer forMode:NSRunLoopCommonModes];
	for (Overlay *o in _overlays.allValues) [o.win orderFrontRegardless];
	[self placeCandles];
}

- (void)togglePause:(NSMenuItem *)item {
	if (_timer) {
		[_timer invalidate];
		_timer = nil;
		for (Overlay *o in _overlays.allValues) [o.win orderOut:nil];
		[self placeCandles];
		item.title = @"Resume";
	} else {
		[self resume];
		item.title = @"Pause";
	}
}

- (void)tick:(NSTimer *)t {
	CFTimeInterval now = CACurrentMediaTime();
	double dt = MIN(now - _last, 0.1);
	_last = now;

	int n = snowAdvance(dt);
	BOOL full = snowFullscreen();
	if (full != _full) { // hide or bring back the candle mouse catcher
		_full = full;
		[self placeCandles];
	}

	NSMutableDictionary<NSNumber *, NSScreen *> *screens = [NSMutableDictionary new];
	for (NSScreen *s in NSScreen.screens) screens[@(screenID(s))] = s;
	NSMutableSet<NSNumber *> *seen = [NSMutableSet new];
	for (int i = 0; i < n; i++) {
		SnowScreen sc;
		snowScreen(i, &sc);
		NSScreen *screen = screens[@(sc.id)];
		if (!screen) continue; // unplugged; Go notices within a second
		[seen addObject:@(sc.id)];
		Overlay *o = _overlays[@(sc.id)];
		if (!o) {
			o = [[Overlay alloc] initWithScreen:screen device:_device queue:_queue dotPipe:_dotPipe triPipe:_triPipe];
			_overlays[@(sc.id)] = o;
			[o.win orderFrontRegardless];
		}
		[o fitScreen:screen];
		int nd = 0, nt = 0;
		float *b = snowFrame(i, &nd, &nt);
		[o draw:b dots:nd tris:nt opacity:_opacity];
	}
	for (NSNumber *k in _overlays.allKeys) {
		if (![seen containsObject:k]) {
			[_overlays[k].win orderOut:nil];
			[_overlays removeObjectForKey:k];
		}
	}
}
@end

void runApp(void) {
	@autoreleasepool {
		NSApplication *app = NSApplication.sharedApplication;
		app.activationPolicy = NSApplicationActivationPolicyAccessory;
		static SnowController *c;
		c = [SnowController new];
		app.delegate = c;
		[c start];
		[app run];
	}
}
