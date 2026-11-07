package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dieqiyun/uni-switch/backend/internal/accounts"
	"path/filepath"
	"time"
)

type Input struct {
	Directory string               `json:"directory"`
	ID        string               `json:"id"`
	Kind      string               `json:"kind"`
	Name      string               `json:"name"`
	Project   string               `json:"project"`
	Value     string               `json:"value"`
	Profile   Profile              `json:"profile"`
	OAuth     accounts.OAuthClient `json:"oauth"`
}

func Dispatch(ctx context.Context, method string, raw []byte) (any, error) {
	var p Input
	if json.Unmarshal(raw, &p) != nil || !filepath.IsAbs(p.Directory) {
		return nil, errors.New("账号或网关参数无效")
	}
	m := accounts.Open(p.Directory)
	switch method {
	case "platform.sources":
		return Profiles(p.Directory, true)
	case "platform.save":
		return Save(p.Directory, p.Profile)
	case "platform.delete":
		return true, Delete(p.Directory, p.ID)
	case "platform.accounts":
		return m.List()
	case "platform.login":
		return m.StartWithOAuth(p.Kind, p.Name, p.Project, p.OAuth)
	case "platform.loginStatus":
		return m.LoginStatus(p.ID)
	case "platform.loginCancel":
		return true, m.Cancel(p.ID)
	case "platform.loginCallback":
		return true, m.Callback(p.ID, p.Value)
	case "platform.accountSwitch":
		return true, m.Edit(p.ID, "switch", "")
	case "platform.accountEnable":
		return true, m.Edit(p.ID, "enable", p.Value)
	case "platform.accountDelete":
		return true, m.Edit(p.ID, "delete", "")
	case "platform.accountProject":
		return true, m.Edit(p.ID, "project", p.Value)
	case "platform.accountRefresh":
		refreshCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		_, e := m.Resolve(refreshCtx, p.ID, true)
		return e == nil, e
	}
	return nil, errors.New("未知账号或网关操作")
}
