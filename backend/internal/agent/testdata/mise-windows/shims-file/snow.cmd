@echo off
rem mise generated shim
setlocal DisableDelayedExpansion
set "shim_path=%~f0"
if /I "%__MISE_SHIM_PATH%"=="%shim_path%" (
  echo mise: recursive shim invocation detected for snow: %shim_path% 1>&2
  exit /b 1
)
set "__MISE_SHIM_PATH=%shim_path%"
setlocal EnableDelayedExpansion
set "__MISE_LAUNCHER=!shim_path!"
set "__MISE_RAW_CMDLINE=!CMDCMDLINE!"
if "!__MISE_RAW_CMDLINE!"=="!__MISE_RAW_CMDLINE:%__MISE_LAUNCHER%=!" goto mise_shim_fallback
mise x -- snow __MISE_LAUNCHER_ARGS__ %*
exit !ERRORLEVEL!
:mise_shim_fallback
set "__MISE_RAW_CMDLINE="
set "__MISE_LAUNCHER="
mise x -- snow __MISE_LAUNCHER_ARGS__ %*
