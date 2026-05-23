.PHONY: build release test clean

# Dev build — windowsgui flag, debug symbols kept, outputs pycalendar.exe at repo root.
build:
	powershell -ExecutionPolicy Bypass -File scripts/build-dev.ps1

# Release build — stripped, outputs to dist/. Pass VERSION=x.y.z to stamp it.
release:
	powershell -ExecutionPolicy Bypass -File scripts/build-windows.ps1 -Version $(or $(VERSION),0.1.0)

test:
	go test ./...

clean:
	if exist pycalendar.exe del pycalendar.exe
	if exist dist\pycalendar.exe del dist\pycalendar.exe
