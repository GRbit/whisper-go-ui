# Installs into ~/.local by default; override with e.g. `make install PREFIX=/usr/local`.
PREFIX ?= $(HOME)/.local
ICON_SIZES := 16 22 24 32 48 64 128 256 512
DESKTOP_FILE := $(PREFIX)/share/applications/whisper-go-ui.desktop

.PHONY: build install uninstall test vet

# The wails.json preBuildHooks entry renders build/icons/*.png and
# build/appicon.png before compiling, so `wails build` is self-contained.
build:
	wails build -tags webkit2_41

install: build
	install -Dm755 build/bin/whisper-go-ui $(PREFIX)/bin/whisper-go-ui
	for size in $(ICON_SIZES); do \
		install -Dm644 build/icons/$$size.png \
			$(PREFIX)/share/icons/hicolor/$${size}x$${size}/apps/whisper-go-ui.png; \
	done
	mkdir -p $(dir $(DESKTOP_FILE))
	printf '%s\n' \
		'[Desktop Entry]' \
		'Type=Application' \
		'Name=Whisper Transcriber' \
		'Comment=Voice transcription with hotkey paste' \
		'Exec=$(PREFIX)/bin/whisper-go-ui' \
		'Icon=whisper-go-ui' \
		'Terminal=false' \
		'Categories=Utility;AudioVideo;' \
		'StartupWMClass=whisper-go-ui' \
		> $(DESKTOP_FILE)
	@# refresh the icon cache only where one already exists (a stale cache hides icons)
	-test -f $(PREFIX)/share/icons/hicolor/icon-theme.cache && \
		gtk-update-icon-cache --force --ignore-theme-index $(PREFIX)/share/icons/hicolor
	@echo "Installed to $(PREFIX)/bin/whisper-go-ui"

uninstall:
	rm -f $(PREFIX)/bin/whisper-go-ui
	rm -f $(DESKTOP_FILE)
	for size in $(ICON_SIZES); do \
		rm -f $(PREFIX)/share/icons/hicolor/$${size}x$${size}/apps/whisper-go-ui.png; \
	done
	@echo "Uninstalled from $(PREFIX)"

test:
	go vet -tags webkit2_41 ./...
	go test -tags webkit2_41 ./...

vet:
	go vet -tags webkit2_41 ./...
