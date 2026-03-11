APP_NAME := multibrowser

.PHONY: build clean desktop-dev desktop-build

build:
	go build -o $(APP_NAME) .

clean:
	rm -f $(APP_NAME)

desktop-dev:
	npm run tauri dev

desktop-build:
	npm run tauri build
