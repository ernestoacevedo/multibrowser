APP_NAME := multibrowser

.PHONY: build clean

build:
	go build -o $(APP_NAME) .

clean:
	rm -f $(APP_NAME)
