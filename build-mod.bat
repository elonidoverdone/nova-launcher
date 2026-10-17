@echo off
rem Builds NovaFX and puts the jar next to NovaLauncher.exe (needs JDK 25: https://adoptium.net).
setlocal
cd /d "%~dp0mod"
call gradlew.bat build || (echo Build failed & pause & exit /b 1)
if not exist "%~dp0dist" mkdir "%~dp0dist"
for %%f in (build\libs\novafx-*.jar) do (
  echo %%~nxf | findstr /i /c:"-sources" >nul || copy /y "%%f" "%~dp0dist\" >nul
)
echo.
echo Done: jar copied to dist\ next to NovaLauncher.exe
pause
