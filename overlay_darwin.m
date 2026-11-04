// Cocoa side: a click-through transparent overlay that draws what the Go
// simulation produces, plus the window/Dock/cursor queries it needs.
// None of these APIs need Screen Recording or Accessibility permission.
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <Metal/Metal.h>
#include <unistd.h>
#include "overlay_darwin.h"
#include "_cgo_export.h"

int listWindows(SnowWin *out, int max) {
	CFArrayRef list = CGWindowListCopyWindowInfo(
		kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
	if (!list) return 0;
	pid_t me = getpid();
	int n = 0;
	for (CFIndex i = 0; i < CFArrayGetCount(list) && n < max; i++) {
		NSDictionary *w = (__bridge NSDictionary *)CFArrayGetValueAtIndex(list, i);
		if ([w[(id)kCGWindowLayer] intValue] != 0) continue;
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

void screenInfo(SnowScreen *out) {
	NSScreen *s = NSScreen.screens.firstObject;
	CGRect d = estimateDock(s);
	*out = (SnowScreen){s.frame.size.width, s.frame.size.height,
	                    d.origin.x, d.origin.y, d.size.width, d.size.height};
}

void cursorPos(double *x, double *y) {
	NSPoint p = NSEvent.mouseLocation;
	*x = p.x;
	*y = NSScreen.screens.firstObject.frame.size.height - p.y;
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

@interface SnowController : NSObject
@end

@implementation SnowController {
	NSWindow *_win;
	CAMetalLayer *_layer;
	id<MTLDevice> _device;
	id<MTLCommandQueue> _queue;
	id<MTLRenderPipelineState> _dotPipe, _triPipe;
	id<MTLBuffer> _dotBuf[kInFlight], _triBuf[kInFlight];
	dispatch_semaphore_t _inflight;
	dispatch_queue_t _renderQ; // Metal drawing happens here, off the main thread
	int _slot;
	BOOL _wasEmpty;
	float _opacity;
	NSTimer *_timer, *_titleTimer;
	CFTimeInterval _last;
	NSStatusItem *_status;
}

- (void)start {
	NSScreen *s = NSScreen.screens.firstObject;
	_win = [[NSWindow alloc] initWithContentRect:s.frame
	                                   styleMask:NSWindowStyleMaskBorderless
	                                     backing:NSBackingStoreBuffered
	                                       defer:NO];
	_win.opaque = NO;
	_win.backgroundColor = NSColor.clearColor;
	_win.hasShadow = NO;
	_win.ignoresMouseEvents = YES;
	_win.level = NSStatusWindowLevel;
	_win.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
	                          NSWindowCollectionBehaviorStationary |
	                          NSWindowCollectionBehaviorFullScreenAuxiliary |
	                          NSWindowCollectionBehaviorIgnoresCycle;

	NSView *v = _win.contentView;
	v.wantsLayer = YES;
	[self setupMetal:v scale:s.backingScaleFactor];

	NSUserDefaults *d = NSUserDefaults.standardUserDefaults;
	[d registerDefaults:@{
		@"amount" : @1.0, @"size" : @1.0, @"opacity" : @1.0, @"piles" : @YES,
		@"lights" : @YES, @"icicles" : @YES, @"wind" : @YES, @"sparkles" : @YES, @"frost" : @YES
	}];

	_status = [NSStatusBar.systemStatusBar statusItemWithLength:NSVariableStatusItemLength];
	[self updateTitle];
	// The countdown changes once a day; checking each minute catches midnight.
	_titleTimer = [NSTimer scheduledTimerWithTimeInterval:60 target:self selector:@selector(updateTitle) userInfo:nil repeats:YES];
	NSMenu *m = [NSMenu new];
	NSMenuItem *about = [m addItemWithTitle:@"About CokeEverywhere" action:@selector(showAbout:) keyEquivalent:@""];
	about.target = self;
	NSMenuItem *repo = [m addItemWithTitle:@"CokeEverywhere on GitHub…" action:@selector(openRepo:) keyEquivalent:@""];
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
	[m addItem:decor];
	[m addItem:NSMenuItem.separatorItem];
	NSMenuItem *loginItem = [m addItemWithTitle:@"Start at Login" action:@selector(toggleLogin:) keyEquivalent:@""];
	loginItem.target = self;
	loginItem.state = snowLoginEnabled() ? NSControlStateValueOn : NSControlStateValueOff;
	NSMenuItem *pause = [m addItemWithTitle:@"Pause" action:@selector(togglePause:) keyEquivalent:@"p"];
	pause.target = self;
	[m addItem:NSMenuItem.separatorItem];
	[m addItemWithTitle:@"Quit CokeEverywhere" action:@selector(terminate:) keyEquivalent:@"q"];
	_status.menu = m;

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

static NSString *const kRepoURL = @"https://github.com/iystoychev/coke-everywhere";

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
		NSAboutPanelOptionApplicationName : @"CokeEverywhere",
		NSAboutPanelOptionCredits : credits,
	}];
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
	});
	_opacity = [d doubleForKey:@"opacity"];
}

- (void)updateTitle {
	char *t = snowTitle();
	_status.button.title = [NSString stringWithUTF8String:t];
	free(t);
}

- (void)setupMetal:(NSView *)v scale:(CGFloat)scale {
	_device = MTLCreateSystemDefaultDevice();
	if (!_device) {
		NSLog(@"CokeEverywhere: no Metal device");
		exit(1);
	}
	_queue = [_device newCommandQueue];
	_layer = [CAMetalLayer layer];
	_layer.device = _device;
	_layer.pixelFormat = MTLPixelFormatBGRA8Unorm;
	_layer.opaque = NO;
	_layer.framebufferOnly = YES;
	_layer.frame = v.bounds;
	_layer.contentsScale = scale;
	_layer.drawableSize = CGSizeMake(v.bounds.size.width * scale, v.bounds.size.height * scale);
	[v.layer addSublayer:_layer];

	NSError *err = nil;
	id<MTLLibrary> lib = [_device newLibraryWithSource:kShaders options:nil error:&err];
	if (!lib) {
		NSLog(@"CokeEverywhere: shader compile failed: %@", err);
		exit(1);
	}
	_dotPipe = [self pipeline:lib vertex:@"dotV" fragment:@"dotF"];
	_triPipe = [self pipeline:lib vertex:@"triV" fragment:@"triF"];
	for (int i = 0; i < kInFlight; i++) {
		_dotBuf[i] = [_device newBufferWithLength:sizeof(float) * SNOW_DOT_FLOATS options:MTLResourceStorageModeShared];
		_triBuf[i] = [_device newBufferWithLength:sizeof(float) * SNOW_TRI_FLOATS options:MTLResourceStorageModeShared];
	}
	_inflight = dispatch_semaphore_create(kInFlight);
	_renderQ = dispatch_queue_create("CokeEverywhere.render", DISPATCH_QUEUE_SERIAL);
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
		NSLog(@"CokeEverywhere: pipeline %@ failed: %@", vf, err);
		exit(1);
	}
	return p;
}

- (void)resume {
	_last = CACurrentMediaTime();
	_timer = [NSTimer timerWithTimeInterval:1.0 / 60 target:self selector:@selector(tick:) userInfo:nil repeats:YES];
	[NSRunLoop.mainRunLoop addTimer:_timer forMode:NSRunLoopCommonModes];
	[_win orderFrontRegardless];
}

- (void)togglePause:(NSMenuItem *)item {
	if (_timer) {
		[_timer invalidate];
		_timer = nil;
		[_win orderOut:nil];
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

	int nd = 0, nt = 0;
	float *b = snowStep(dt, &nd, &nt); // dots at b, triangles at b + SNOW_DOT_FLOATS

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
	Uniforms u = {sz.width, sz.height, _opacity, 0};
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

void runApp(void) {
	@autoreleasepool {
		NSApplication *app = NSApplication.sharedApplication;
		app.activationPolicy = NSApplicationActivationPolicyAccessory;
		static SnowController *c;
		c = [SnowController new];
		[c start];
		[app run];
	}
}
