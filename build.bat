@echo off
set CGO_ENABLED=0
go run ./tools/genres 1.2.0 || exit /b 1
go build -trimpath -ldflags "-H windowsgui -s -w" -o PelicanConsole.exe . || exit /b 1
echo Built PelicanConsole.exe
