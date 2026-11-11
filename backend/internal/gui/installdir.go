//go:build !nogui

package gui

import (
	"errors"
	"fmt"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// The Windows app opened from Downloads installs itself (update.Settle),
// and the first time it asks where, in a dialog of Windows' own
// (installdir_windows.go): the folder, Install, Change… and Cancel, as an
// installer's folder page has them (feng_21795_78266 on Discord).

// installWords are the dialog's words in one language: its title, the
// question, the two buttons, the folder picker's title, and what is said
// when the folder can't be used: one only an administrator can write
// to, one downloads land in, and any other failure.
type installWords struct {
	title, ask, install, change, pick string
	admin, downloads, failed          string
}

var installText = map[string]installWords{
	"en": {
		title: "uni-switch", ask: "Install uni-switch in this folder?", install: "Install", change: "Change…",
		pick:      "Choose where to install uni-switch",
		admin:     "Only an administrator can write to %s. uni-switch is installed for you alone and updates itself, so choose a folder of your own.",
		downloads: "Downloads land in %s. Choose another folder.",
		failed:    "uni-switch can't be installed in %s: %v",
	},
	"zh": {
		title: "uni-switch", ask: "把 uni-switch 安装到这个文件夹？", install: "安装", change: "更改…",
		pick:      "选择 uni-switch 的安装位置",
		admin:     "只有管理员才能写入 %s。uni-switch 只为你安装，并会自己更新，请选择一个你自己的文件夹。",
		downloads: "%s 是下载存放的地方，请选择另一个文件夹。",
		failed:    "uni-switch 无法安装到 %s：%v",
	},
	"zh-TW": {
		title: "uni-switch", ask: "把 uni-switch 安裝到這個資料夾？", install: "安裝", change: "變更…",
		pick:      "選擇 uni-switch 的安裝位置",
		admin:     "只有系統管理員才能寫入 %s。uni-switch 只為你安裝，並會自己更新，請選擇一個你自己的資料夾。",
		downloads: "%s 是下載存放的地方，請選擇另一個資料夾。",
		failed:    "uni-switch 無法安裝到 %s：%v",
	},
	"ja": {
		title: "uni-switch", ask: "このフォルダーに uni-switch をインストールしますか？", install: "インストール", change: "変更…",
		pick:      "uni-switch のインストール先を選択",
		admin:     "%s に書き込めるのは管理者だけです。uni-switch はあなた専用にインストールされ、自分で更新するので、あなた自身のフォルダーを選んでください。",
		downloads: "%s はダウンロードの保存先です。別のフォルダーを選んでください。",
		failed:    "%s に uni-switch をインストールできません：%v",
	},
	"de": {
		title: "uni-switch", ask: "uni-switch in diesem Ordner installieren?", install: "Installieren", change: "Ändern…",
		pick:      "Speicherort für uni-switch wählen",
		admin:     "Nur ein Administrator darf in %s schreiben. uni-switch wird nur für Sie installiert und aktualisiert sich selbst, wählen Sie also einen eigenen Ordner.",
		downloads: "In %s landen Downloads. Wählen Sie einen anderen Ordner.",
		failed:    "uni-switch kann nicht in %s installiert werden: %v",
	},
}

// installWordsFor is the dialog's words in lang (trayLang's), English
// where it has none.
func installWordsFor(lang string) installWords {
	if w, ok := installText[lang]; ok {
		return w
	}
	return installText["en"]
}

// refusal is what the dialog says about dir, which check refused with err.
func (w installWords) refusal(dir string, err error) string {
	switch {
	case errors.Is(err, update.ErrNeedsAdmin):
		return fmt.Sprintf(w.admin, dir)
	case errors.Is(err, update.ErrDownloadFolder):
		return fmt.Sprintf(w.downloads, dir)
	}
	return fmt.Sprintf(w.failed, dir, err)
}
