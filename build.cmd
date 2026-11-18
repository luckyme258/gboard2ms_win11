@echo off
setlocal

rem ============================================================
rem  gboard2ms - Go GUI build script (project standard)
rem  release : go build -ldflags "-s -w -H=windowsgui"  => ~6.7 MB
rem  debug   : go build -ldflags "-H=windowsgui"        => ~10.4 MB
rem  usage   : build.cmd          (release, default)
rem             build.cmd debug  (with DWARF / symbols)
rem ============================================================

set "GOROOT=E:\codes\_toolchains\go"
set "GOPATH=E:\codes\_toolchains\gopath"
set "GOCACHE=E:\codes\_toolchains\gocache"
set "GOPROXY=https://goproxy.cn,direct"
set "GOSUMDB=sum.golang.google.cn"
set "GOEXE=%GOROOT%\bin\go.exe"

cd /d "%~dp0"

if not exist "%GOEXE%" (
  echo [fail] go toolchain not found: %GOEXE%
  exit /b 1
)

if /I "%1"=="debug" (
  echo [build] DEBUG  ^(DWARF + symtab, about 10.4 MB^)
  "%GOEXE%" build -ldflags "-H=windowsgui" -o walkui.exe ./cmd/walkui
  if errorlevel 1 goto fail
) else (
  echo [build] RELEASE  ^(-s -w, about 6.7 MB, this is the shippable one^)
  "%GOEXE%" build -ldflags "-s -w -H=windowsgui" -o walkui.exe ./cmd/walkui
  if errorlevel 1 goto fail
)

for %%F in (walkui.exe) do echo [ok] %%F  %%~zF bytes
echo [note] manifest changed? rerun: rsrc.exe -manifest cmd\walkui\app.manifest -o cmd\walkui\rsrc.syso
goto :eof

:fail
echo [fail] build error
exit /b 1
