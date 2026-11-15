package appdir

import (
	"os"
	"path/filepath"
	"runtime"
)

// HermesHome is the folder Hermes Agent (Nous Research) keeps its files in:
// config.yaml, .env, plugins, skills, profiles and state.db. It is
// $HERMES_HOME when that is set, else the platform's default, as
// hermes_constants.get_hermes_home reads them: %LOCALAPPDATA%\hermes on
// Windows (AppData\Local\hermes under the home folder without
// LOCALAPPDATA), ~/.hermes everywhere else. Every place uni-switch reads or
// writes Hermes's files goes through here (新v学员 on Discord: on native
// Windows Hermes and rtk's hook were in %LOCALAPPDATA%\hermes, and uni-switch
// looked for them in ~/.hermes).
func HermesHome() string {
	home, _ := os.UserHomeDir()
	return HermesHomeIn(home)
}

// HermesHomeIn is HermesHome with home as the user's home folder.
func HermesHomeIn(home string) string {
	if d := Getenv("HERMES_HOME"); d != "" {
		return d
	}
	return HermesDefaultIn(home)
}

// HermesDefaultIn is Hermes's own default home on this machine, whatever
// HERMES_HOME says (hermes_constants._get_platform_default_hermes_home):
// the root a HERMES_HOME under it is a profile of.
func HermesDefaultIn(home string) string {
	return hermesDefault(runtime.GOOS, home, Getenv("LOCALAPPDATA"))
}

func hermesDefault(goos, home, localAppData string) string {
	if goos != "windows" {
		return filepath.Join(home, ".hermes")
	}
	if localAppData == "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(localAppData, "hermes")
}
