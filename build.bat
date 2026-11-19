@echo off
set CGO_ENABLED=0
go build -trimpath -ldflags "-H windowsgui -s -w" -o PelicanConsole.exe . || exit /b 1
echo Built PelicanConsole.exe
