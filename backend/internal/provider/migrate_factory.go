package provider

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Factory's accounts on the retired opencode-factory-auth, as its
// device sign-in keeps one: the WorkOS pair as access and refresh (the
// refresh token rotates, so it moves rather than being copied), the org
// and region droid needs beside them.
func init() {
	movers["factory"] = &mover{

		agents: []string{"factory"},
		out: func() ([]Moving, error) {
			ls := factoryLogins()
			factoryMu.Lock()
			defer factoryMu.Unlock()
			var out []Moving
			for _, l := range ls {
				c := l.creds
				if c.Key {
					// an API key (fk-…) is the plugin's api account, as its
					// own "Factory API key" sign-in saves one: an oauth one
					// would be renewed with no refresh token and lapse
					out = append(out, Moving{User: l.User, First: l.Active, On: l.On, Lapsed: l.Lapsed != "", Plan: l.Plan, Auth: map[string]any{
						"type": "api",
						"key":  c.Access,
						"metadata": map[string]any{
							"email":        c.Email,
							"userId":       c.UserID,
							"region":       c.Region,
							"premBaseHost": c.Prem,
						},
					}})
					continue
				}
				out = append(out, Moving{User: l.User, First: l.Active, On: l.On, Lapsed: l.Lapsed != "", Plan: l.Plan, Auth: map[string]any{
					"type":                 "oauth",
					"access":               c.Access,
					"refresh":              c.Refresh,
					"expires":              c.ExpiresAt,
					"orgId":                c.Org,
					"email":                c.Email,
					"userId":               c.UserID,
					"activeOrganizationId": c.Active,
					"region":               c.Region,
					"premBaseHost":         c.Prem,
					"accountId":            l.User,
				}})
			}
			return out, nil
		},
		back: func(ls []savedLogin, user string, auth map[string]any) ([]savedLogin, string, error) {
			if str(auth["type"]) == "api" {
				return factoryKeyBack(ls, user, auth)
			}
			access := str(auth["access"])
			if access == "" {
				return ls, "", errors.New("Factory: an unreadable plugin sign-in")
			}
			if user == "" {
				user = firstNonEmpty(str(auth["accountId"]), str(auth["email"]), str(auth["userId"]))
			}
			i := backInto(&ls, "factory", user)
			c, _ := factorySaved(ls[i])
			c.Access, c.Refresh, c.ExpiresAt = access, str(auth["refresh"]), num(auth["expires"])
			c.Org = firstNonEmpty(str(auth["orgId"]), c.Org)
			c.Active = firstNonEmpty(str(auth["activeOrganizationId"]), c.Active)
			c.Email = firstNonEmpty(str(auth["email"]), c.Email)
			c.UserID = firstNonEmpty(str(auth["userId"]), c.UserID)
			c.Region = firstNonEmpty(str(auth["region"]), c.Region)
			c.Prem = firstNonEmpty(str(auth["premBaseHost"]), c.Prem)
			b, err := json.Marshal(c)
			if err != nil {
				return ls, "", err
			}
			ls[i].Auth, ls[i].Lapsed, ls[i].Seen = b, "", time.Now().UTC().Truncate(time.Second)
			return ls, ls[i].User, nil
		},
	}
}

// factoryKeyBack keeps the plugin's api account (an fk- key) as the
// built-in's key account (factory_key.go).
func factoryKeyBack(ls []savedLogin, user string, auth map[string]any) ([]savedLogin, string, error) {
	key := strings.TrimSpace(str(auth["key"]))
	if !strings.HasPrefix(key, factoryKeyPrefix) {
		return ls, "", errors.New("Factory: an unreadable plugin API key")
	}
	md, _ := auth["metadata"].(map[string]any)
	if user == "" {
		user = firstNonEmpty(str(md["email"]), str(md["userId"]), factoryKeyName(key))
	}
	i := backInto(&ls, "factory", user)
	c := factoryCreds{Access: key, Key: true, Email: str(md["email"]), UserID: str(md["userId"]),
		Region: str(md["region"]), Prem: str(md["premBaseHost"])}
	b, err := json.Marshal(c)
	if err != nil {
		return ls, "", err
	}
	ls[i].Auth, ls[i].Lapsed, ls[i].Seen = b, "", time.Now().UTC().Truncate(time.Second)
	return ls, ls[i].User, nil
}
