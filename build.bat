@echo off
rem Local build.  build.bat           -> dev build (no auto-update prompts)
rem               build.bat 1.4.0     -> versioned build
set VER=%1
set RES=%1
if "%VER%"=="" ( set VER=dev& set RES=0.0.0 )
go run ./tools/genres %RES% || exit /b 1
set CGO_ENABLED=0
go build -trimpath -ldflags "-H windowsgui -s -w -X main.appVersion=%VER%" -o PelicanConsole.exe . || exit /b 1
echo Built PelicanConsole.exe (%VER%)
