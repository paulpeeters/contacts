@echo off
setlocal enabledelayedexpansion

echo --------------------------------------------
echo Build Contacts
echo --------------------------------------------
echo.

:: Repo root = parent of this script's own folder (build\) -- computed
:: from the script's own location rather than hardcoded, so this works no
:: matter where the repo is cloned to.
set SCRIPT_DIR=%~dp0
for %%I in ("%SCRIPT_DIR%..") do set PROJECT_DIR=%%~fI

:: --------------------------------------------
:: build.env (optional, gitignored -- see build.env.template for the
:: format and how to set it up). Defines where each target's build output
:: gets copied to. If build.env doesn't exist, or a given *_DEPLOY_DIR
:: isn't set in it, the build still runs -- the output just stays in
:: %PROJECT_DIR% instead of being copied anywhere.
:: --------------------------------------------
set WINDOWS_DEPLOY_DIR=
set LINUX_DEPLOY_DIR=
set DOCKERHUB_IMAGE=

if exist "%SCRIPT_DIR%build.env" (
  for /f "usebackq tokens=1,* delims==" %%A in ("%SCRIPT_DIR%build.env") do (
    set "line=%%A"
    if not "!line:~0,1!"=="#" if not "!line!"=="" set "%%A=%%B"
  )
) else (
  echo NOTE: build\build.env not found -- build output will stay in
  echo %PROJECT_DIR% instead of being copied anywhere. Copy
  echo build\build.env.template to build\build.env and fill in your own
  echo paths if you want it copied to a staging folder automatically.
  echo.
)

:: TARGET
set TARGET=windows
set /p INPUT_TARGET=Target (windows/linux/docker) [windows]:
if not "%INPUT_TARGET%"=="" set TARGET=%INPUT_TARGET%

echo.
echo --------------------------------------------
echo Target: %TARGET%
echo --------------------------------------------
echo.

pushd "%PROJECT_DIR%"

if /i "%TARGET%"=="windows" goto :build_windows
if /i "%TARGET%"=="linux"   goto :build_linux
if /i "%TARGET%"=="docker"  goto :build_docker

echo Unknown target: %TARGET% ^(expected windows, linux or docker^)
popd
exit /b 1

:build_windows
echo Building contacts.exe (windows/amd64, GUI build)...
go build -ldflags "-H=windowsgui -s -w" -o contacts.exe .
if errorlevel 1 goto :build_failed
echo Build OK: %PROJECT_DIR%\contacts.exe

if not "%WINDOWS_DEPLOY_DIR%"=="" (
  if not exist "%WINDOWS_DEPLOY_DIR%" mkdir "%WINDOWS_DEPLOY_DIR%"
  copy /y contacts.exe "%WINDOWS_DEPLOY_DIR%\contacts.exe" >nul
  echo Copied to: %WINDOWS_DEPLOY_DIR%\contacts.exe
) else (
  echo WINDOWS_DEPLOY_DIR not set in build.env -- not copied anywhere.
)
goto :build_done

:build_linux
echo Building contacts (linux/amd64)...
echo This is the artifact for the remote/Dockhand deploy -- see the
echo separately maintained deploy script, which ships this from
echo LINUX_DEPLOY_DIR to the target server.
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
go build -ldflags="-s -w" -o contacts .
set BUILD_ERR=%ERRORLEVEL%
set GOOS=
set GOARCH=
set CGO_ENABLED=
if not "%BUILD_ERR%"=="0" goto :build_failed
echo Build OK: %PROJECT_DIR%\contacts

if not "%LINUX_DEPLOY_DIR%"=="" (
  if not exist "%LINUX_DEPLOY_DIR%" mkdir "%LINUX_DEPLOY_DIR%"
  copy /y contacts "%LINUX_DEPLOY_DIR%\contacts" >nul
  echo Copied to: %LINUX_DEPLOY_DIR%\contacts
) else (
  echo LINUX_DEPLOY_DIR not set in build.env -- not copied anywhere.
)
goto :build_done

:build_docker
echo Building local test image (contacts:latest)...
echo This is for LOCAL testing only (docker compose up, on this machine's
echo Docker Desktop) -- unrelated to the remote/Dockhand deploy, which
echo uses the "linux" target instead and never builds a custom image.
docker build -t contacts:latest .
if errorlevel 1 goto :build_failed
echo Build OK: contacts:latest ^(local Docker image^)
echo Run it with: docker compose up -d

echo.
set PUSH=no
set /p INPUT_PUSH=Push contacts:latest to Docker Hub? (yes/no) [no]:
if not "%INPUT_PUSH%"=="" set PUSH=%INPUT_PUSH%

if /i "%PUSH%"=="yes" (
  if "%DOCKERHUB_IMAGE%"=="" (
    echo DOCKERHUB_IMAGE not set in build.env -- skipping push. Add e.g.
    echo DOCKERHUB_IMAGE=yourusername/contacts to build\build.env and
    echo re-run if you want this to work.
  ) else (
    echo Tagging and pushing %DOCKERHUB_IMAGE%:latest ...
    docker tag contacts:latest %DOCKERHUB_IMAGE%:latest
    docker push %DOCKERHUB_IMAGE%:latest
    if errorlevel 1 (
      echo WARNING: docker push failed -- are you logged in? Run
      echo "docker login" first, then re-run this and choose to push again.
    ) else (
      echo Pushed: %DOCKERHUB_IMAGE%:latest
    )
  )
)
goto :build_done

:build_failed
echo.
echo Build failed ^(exit code %ERRORLEVEL%^).
set GOOS=
set GOARCH=
set CGO_ENABLED=
popd
pause
exit /b 1

:build_done
popd
echo.
echo --------------------------------------------
echo Done.
echo --------------------------------------------
pause
