APP := build/SnowDesk.app
BIN := $(APP)/Contents/MacOS/SnowDesk
VERSION := $(shell plutil -extract CFBundleShortVersionString raw packaging/Info.plist)
ZIP := build/SnowDesk-$(VERSION).zip

.PHONY: app install release test icon clean

# Build the app bundle in build/.
app:
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	go build -o build/SnowDesk-host .
	cp build/SnowDesk-host $(BIN) # cp, not go build -o: BIN may be a universal binary from `make release`
	cp packaging/Info.plist $(APP)/Contents/Info.plist
	cp packaging/AppIcon.icns $(APP)/Contents/Resources/AppIcon.icns
	codesign --force --sign - $(APP)

# Copy the app into /Applications, updating an older copy in place.
install: app
	ditto $(APP) /Applications/SnowDesk.app

# Build a universal (Apple Silicon + Intel) app and zip it for a release.
release: app
	CGO_ENABLED=1 GOARCH=arm64 CC="clang -arch arm64" go build -o build/SnowDesk-arm64 .
	CGO_ENABLED=1 GOARCH=amd64 CC="clang -arch x86_64" go build -o build/SnowDesk-amd64 .
	lipo -create -output $(BIN) build/SnowDesk-arm64 build/SnowDesk-amd64
	codesign --force --sign - $(APP)
	ditto -c -k --keepParent $(APP) $(ZIP)
	@echo "built $(ZIP)"

test:
	go vet ./...
	go test ./...

# Redraw packaging/AppIcon.icns from packaging/icon.
icon:
	mkdir -p build/AppIcon.iconset
	go run ./packaging/icon build/icon-1024.png
	for s in 16 32 128 256 512; do \
		sips -z $$s $$s build/icon-1024.png --out build/AppIcon.iconset/icon_$${s}x$${s}.png >/dev/null; \
		sips -z $$((s*2)) $$((s*2)) build/icon-1024.png --out build/AppIcon.iconset/icon_$${s}x$${s}@2x.png >/dev/null; \
	done
	iconutil -c icns -o packaging/AppIcon.icns build/AppIcon.iconset
	sips -z 256 256 build/icon-1024.png --out packaging/icon-256.png >/dev/null # README logo

clean:
	rm -rf ./build
