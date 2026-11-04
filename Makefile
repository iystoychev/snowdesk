APP := build/CokeEverywhere.app
BIN := $(APP)/Contents/MacOS/CokeEverywhere

.PHONY: app install test icon clean

# Build the app bundle in build/.
app:
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	go build -o $(BIN) .
	cp packaging/Info.plist $(APP)/Contents/Info.plist
	cp packaging/AppIcon.icns $(APP)/Contents/Resources/AppIcon.icns
	codesign --force --sign - $(APP)

# Copy the app into /Applications, updating an older copy in place.
install: app
	ditto $(APP) /Applications/CokeEverywhere.app

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

clean:
	rm -rf ./build
