// Package plugin retains only compatibility shapes for legacy provider records.
// It never reads extension packages or authentication, installs dependencies,
// starts a host, or runs third-party code. Discovery is empty; execution and
// mutation return ErrUnavailable. Existing user files are left untouched.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

var ErrUnavailable = errors.New("third-party extensions have been removed")

type GatewayContext struct {
	Origin   string `json:"origin"`
	Protocol string `json:"protocol"`
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Path     string `json:"path"`
	Agent    string `json:"agent"`
	Session  string `json:"session"`
	Cwd      string `json:"cwd"`
	Query    string `json:"query"`
}
type GatewayRun struct {
	Body json.RawMessage
}
type OwnCall struct {
	Kind       string `json:"kind"`
	Model      string `json:"model"`
	Input      int    `json:"input"`
	Output     int    `json:"output"`
	CacheRead  int    `json:"cacheRead"`
	CacheWrite int    `json:"cacheWrite"`
	Status     int    `json:"status"`
	Millis     int64  `json:"ms"`
	Error      string `json:"error"`
}
type Loaded struct {
	Spec  string `json:"spec"`
	Error string `json:"error,omitempty"`

	Provides []string `json:"provides,omitempty"`

	ServedBy map[string]string `json:"servedBy,omitempty"`

	Tools []Tool `json:"tools,omitempty"`

	Searches []SearchDef `json:"searches,omitempty"`
}
type FetchRequest struct {
	Provider string            `json:"provider"`
	Account  string            `json:"account,omitempty"`
	Model    string            `json:"model"`
	NPM      string            `json:"npm"`
	URL      string            `json:"url"`
	Method   string            `json:"method"`
	Headers  map[string]string `json:"headers"`
	Body     []byte            `json:"-"`
	Session  string            `json:"session,omitempty"`

	Proxy string `json:"proxy,omitempty"`
}
type NPM struct {
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	License     string `json:"license,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Weekly      int    `json:"weekly"`

	Icon string `json:"icon,omitempty"`
}
type Page struct {
	Readme  string    `json:"readme"`
	Updated time.Time `json:"updated"`
}
type WindowSpec struct {
	Page   *PageSpec
	Panels []PanelSpec

	Actions []ActionSpec

	Reads []string
}
type PageSpec struct {
	Title string

	Titles map[string]string
	Icon   string
	File   string
	Dir    string

	Replaces string

	Service *ServiceSpec

	KeepAlive bool
}
type ServiceSpec struct {
	Port   int
	Option string
}
type PanelSpec struct {
	Slot   string
	Title  string
	Titles map[string]string
	File   string
	Dir    string
}
type ActionSpec struct {
	ID     string
	Menu   string
	Title  string
	Titles map[string]string
}

const NewAccount = "new"

type Method struct {
	Type  string `json:"type"`
	Label string `json:"label"`

	Placeholder string `json:"placeholder,omitempty"`

	Keyless bool `json:"keyless,omitempty"`
}
type Cost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	Cache  struct {
		Read  float64 `json:"read"`
		Write float64 `json:"write"`
	} `json:"cache"`
}
type Model struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	NPM       string   `json:"npm"`
	URL       string   `json:"url"`
	APIID     string   `json:"apiId"`
	Context   int      `json:"context"`
	Input     int      `json:"input"`
	Output    int      `json:"output"`
	Reasoning bool     `json:"reasoning"`
	Image     bool     `json:"image"`
	Released  string   `json:"released"`
	Variants  []string `json:"variants"`

	Free bool `json:"free"`

	Rate    float64 `json:"rate"`
	RateWas float64 `json:"rateWas"`

	Cost *Cost `json:"cost"`

	ImageSaid bool `json:"imageSaid"`

	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`

	OutputsSaid bool `json:"outputsSaid,omitempty"`

	Temperature *bool `json:"temperature,omitempty"`

	InputsSaid bool `json:"inputsSaid,omitempty"`

	Fast bool `json:"fast"`

	ServiceTiers []string `json:"serviceTiers,omitempty"`

	Decides bool `json:"decides,omitempty"`

	Searches bool `json:"searches,omitempty"`
}
type Provider struct {
	ID      string   `json:"id"`
	Spec    string   `json:"spec"`
	Name    string   `json:"name"`
	NPM     string   `json:"npm"`
	API     string   `json:"api"`
	Methods []Method `json:"methods"`

	Icon string `json:"icon,omitempty"`

	Usage bool `json:"usage"`

	Checkin bool `json:"checkin,omitempty"`

	Renews    bool  `json:"renews,omitempty"`
	RenewLead int64 `json:"renewLead,omitempty"`

	Options []Option `json:"options,omitempty"`

	Decide bool `json:"decide,omitempty"`

	Searches  bool    `json:"searches,omitempty"`
	SignedIn  bool    `json:"signedIn"`
	AuthType  string  `json:"authType"`
	AccountID string  `json:"accountId"`
	Models    []Model `json:"models"`

	FellBack bool `json:"fellBack,omitempty"`

	ListError string `json:"listError,omitempty"`

	Accounts []Account `json:"accounts"`

	Single bool `json:"single,omitempty"`

	MaxConcurrency int `json:"maxConcurrency,omitempty"`

	MaxAccounts int `json:"maxAccounts,omitempty"`
}
type Option struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`

	Confirm string `json:"confirm,omitempty"`

	Labels       map[string]string `json:"labels,omitempty"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	Confirms     map[string]string `json:"confirms,omitempty"`

	FiltersModels bool `json:"filtersModels,omitempty"`

	Placement string `json:"placement,omitempty"`
}
type Account struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	AccountID string `json:"accountId"`

	Hint string `json:"hint,omitempty"`

	Models    []string `json:"models,omitempty"`
	FellBack  bool     `json:"fellBack,omitempty"`
	ListError string   `json:"listError,omitempty"`
}

var ProxyFor func(provider, key string) string
var ListingOff func() []string

type KeptSignedIn struct {
	Due     bool   `json:"due"`
	Renewed bool   `json:"renewed"`
	Waiting bool   `json:"waiting"`
	Gone    bool   `json:"gone"`
	Err     string `json:"error"`
}
type Authorization struct {
	Session      string `json:"session"`
	URL          string `json:"url"`
	Instructions string `json:"instructions"`
	Method       string `json:"method"`

	Opened bool `json:"opened"`
}
type Saved struct {
	Provider string `json:"provider"`
	Account  string `json:"account"`
}
type Options struct {
	BaseURL string            `json:"baseURL"`
	APIKey  string            `json:"apiKey"`
	Headers map[string]string `json:"headers"`
	Fetch   bool              `json:"fetch"`
}
type Checked struct {
	Models  []string `json:"models"`
	Usage   *Usage   `json:"usage"`
	Refused string   `json:"refused"`
}
type Usage struct {
	Plan    string        `json:"plan"`
	Until   string        `json:"until"`
	Renew   string        `json:"renew"`
	Balance string        `json:"balance"`
	Error   string        `json:"error"`
	User    string        `json:"user"`
	SignIn  string        `json:"signIn"`
	Windows []UsageWindow `json:"windows"`
	Resets  *UsageResets  `json:"resets"`
}
type UsageResets struct {
	Count    int    `json:"count"`
	Until    string `json:"until"`
	ByWindow bool   `json:"byWindow"`
	FiveHour int    `json:"fiveHour"`
	Weekly   int    `json:"weekly"`
	Team     bool   `json:"team"`
	Each     []struct {
		Until  string `json:"until"`
		Window string `json:"window"`
	} `json:"each"`
}
type UsageWindow struct {
	Name      string   `json:"name"`
	Used      *float64 `json:"used"`
	ResetsAt  string   `json:"resetsAt"`
	ResetSecs int64    `json:"resetSecs"`
	Display   string   `json:"display"`
	Amount    float64  `json:"amount"`
	Limit     float64  `json:"limit"`
	Unit      string   `json:"unit"`
	Span      float64  `json:"span"`
	Model     string   `json:"model"`
	Models    []string `json:"models"`
	NotModels []string `json:"notModels"`
	Aside     bool     `json:"aside"`

	RunsOutAt string  `json:"runsOutAt"`
	RunsOut   float64 `json:"runsOut"`

	Balance *float64 `json:"balance"`
}
type Checkin struct {
	Outcome string  `json:"outcome"`
	Credit  float64 `json:"credit"`
	Streak  int     `json:"streak"`
	Message string  `json:"message"`
	Until   string  `json:"until"`
}

var SubscriptionPlugin func(id string) string = func(id string) string { var zero0 string; return zero0 }

type SearchDef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	KeysURL     string `json:"keysURL,omitempty"`
	KeyOptional bool   `json:"keyOptional,omitempty"`

	NeedURL bool `json:"needURL,omitempty"`

	Auth string `json:"auth,omitempty"`
}
type PluginSearch struct {
	Spec string `json:"spec"`
	SearchDef
}
type SearchPage struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Text  string `json:"text"`
	Age   string `json:"age"`
}
type SearchRefused struct {
	Status     int
	Msg        string
	RetryAfter string
}
type Entry struct {
	Spec string `json:"spec"`

	Off bool `json:"off,omitempty"`

	Options map[string]any `json:"options,omitempty"`
}
type List struct {
	Plugins []Entry `json:"plugins"`

	Config map[string]any `json:"config,omitempty"`

	Prefer map[string]string `json:"prefer,omitempty"`
}
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	Schema json.RawMessage `json:"schema,omitempty"`
}
type PluginTools struct {
	Spec  string `json:"spec"`
	Tools []Tool `json:"tools"`
}

var ToolsNoted func()

type Waiting struct {
	Spec    string `json:"spec"`
	Package string `json:"package"`
	Version string `json:"version"`
	Latest  string `json:"latest"`
	Git     bool   `json:"git,omitempty"`
}
type Updated struct {
	Package string    `json:"package"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	At      time.Time `json:"at"`
}

func Agent(target string) (file string, only bool) {
	var zero0 string
	var zero1 bool
	return zero0, zero1
}

func HasBun() bool { var zero0 bool; return zero0 }

func AuthEntries() (map[string]json.RawMessage, error) {
	var zero0 map[string]json.RawMessage
	var zero1 error
	return zero0, zero1
}

func AuthWho(raw json.RawMessage) string { var zero0 string; return zero0 }

func AuthExpires(raw json.RawMessage) int64 { var zero0 int64; return zero0 }

func EditAuth(f func(m map[string]json.RawMessage) (bool, error)) error { return ErrUnavailable }

func HasGateway() bool { var zero0 bool; return zero0 }

func GatewayBegin(ctx context.Context, body []byte, info GatewayContext) *GatewayRun {
	var zero0 *GatewayRun
	return zero0
}

func (r *GatewayRun) Finish(ctx context.Context, reply json.RawMessage, success bool, request ...json.RawMessage) {
}

func (r *GatewayRun) Close() {}

func OnChange(f func()) {}

func OnChangeNow(f func()) {}

func OnSignIn(f func(provider, account, said string)) {}

func OnUsage(f func(provider, account string, u OwnCall)) {}

func OnListed(f func()) {}

func Running() bool { var zero0 bool; return zero0 }

func Plugins(ctx context.Context) ([]Loaded, error) {
	var zero0 []Loaded
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func Fetch(ctx context.Context, r FetchRequest) (*http.Response, error) {
	var zero0 *http.Response
	return zero0, ErrUnavailable
}
func ModelLoad(provider, model string, now time.Time) (pct float64, at time.Time, ok bool) {
	var zero0 float64
	var zero1 time.Time
	var zero2 bool
	return zero0, zero1, zero2
}

func FreshLoads(now time.Time) map[string]map[string]float64 {
	var zero0 map[string]map[string]float64
	return zero0
}

func Readme(ctx context.Context, name string) (Page, error) {
	var zero0 Page
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func Upgrade(ctx context.Context, name string) error { return ErrUnavailable }

func Icon(spec, id string) string { var zero0 string; return zero0 }
func Middleware(target string) (file string, only bool) {
	var zero0 string
	var zero1 bool
	return zero0, zero1
}

func ListStamp() string { var zero0 string; return zero0 }

func Window(target string) (w *WindowSpec, only bool) {
	var zero0 *WindowSpec
	var zero1 bool
	return zero0, zero1
}

func (s *ServiceSpec) ServicePort(options map[string]any) int { var zero0 int; return zero0 }

func (m Method) KeyTitle(name string) string { var zero0 string; return zero0 }

func ListedAt() (time.Time, bool) { var zero0 time.Time; var zero1 bool; return zero0, zero1 }

func Providers(ctx context.Context) ([]Provider, error) {
	var zero0 []Provider
	var zero1 error
	return zero0, zero1
}

func Listed() ([]Provider, bool) { var zero0 []Provider; var zero1 bool; return zero0, zero1 }

func Cached() []Provider { var zero0 []Provider; return zero0 }

func RenewAt(p Provider, key string) (time.Time, bool) {
	var zero0 time.Time
	var zero1 bool
	return zero0, zero1
}

func KeepSignedIn(ctx context.Context, provider, account string) (KeptSignedIn, error) {
	var zero0 KeptSignedIn
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func ProviderOf(key string) string { var zero0 string; return zero0 }

func SignedIn(provider string) bool { var zero0 bool; return zero0 }

func Authorize(ctx context.Context, provider string, method int, inputs map[string]string, account string) (Authorization, error) {
	var zero0 Authorization
	return zero0, ErrUnavailable
}

func Finish(ctx context.Context, session, code string) (Saved, error) {
	var zero0 Saved
	return zero0, ErrUnavailable
}

func APIKey(ctx context.Context, provider string, method int, inputs map[string]string, key, account string) (Saved, error) {
	var zero0 Saved
	return zero0, ErrUnavailable
}

func SignOut(ctx context.Context, provider, account string) error { return ErrUnavailable }

func LoaderOptions(ctx context.Context, provider, account string) (Options, error) {
	var zero0 Options
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func Import(ctx context.Context, provider string, auth map[string]any) (string, error) {
	var zero0 string
	return zero0, ErrUnavailable
}

func Check(ctx context.Context, provider, account string) (Checked, error) {
	var zero0 Checked
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func Auths(provider string) map[string]map[string]any {
	var zero0 map[string]map[string]any
	return zero0
}

func Take(ctx context.Context, provider string, keys []string) (map[string]map[string]any, error) {
	var zero0 map[string]map[string]any
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func Restore(ctx context.Context, provider, key string, auth map[string]any) error {
	return ErrUnavailable
}

func AccountCheckin(ctx context.Context, provider, account string) (Checkin, error) {
	var zero0 Checkin
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func AccountUsage(ctx context.Context, provider, account string) (Usage, error) {
	var zero0 Usage
	var zero1 error = ErrUnavailable
	return zero0, zero1
}

func (p PluginSearch) VendorID() string { var zero0 string; return zero0 }

func (e SearchRefused) Error() string { return "third-party extensions have been removed" }

func RunSearch(ctx context.Context, spec, id, query string, count int, key, url string) ([]SearchPage, error) {
	var zero0 []SearchPage
	return zero0, ErrUnavailable
}

func RunSearchAs(ctx context.Context, spec, id, query string, count int, account string) ([]SearchPage, error) {
	var zero0 []SearchPage
	return zero0, ErrUnavailable
}

func NotedSearches() ([]PluginSearch, error) {
	var zero0 []PluginSearch
	var zero1 error
	return zero0, zero1
}
func Dir() string { var zero0 string; return zero0 }

func Load() List { var zero0 List; return zero0 }

func IsPath(spec string) bool { var zero0 bool; return zero0 }

func IsGit(spec string) bool { var zero0 bool; return zero0 }

func Name(spec string) string { var zero0 string; return zero0 }

func Target(spec string) string { var zero0 string; return zero0 }

func Add(ctx context.Context, spec string) (Entry, error) {
	var zero0 Entry
	return zero0, ErrUnavailable
}

func AddExact(ctx context.Context, spec string) (Entry, error) {
	var zero0 Entry
	return zero0, ErrUnavailable
}

func Remove(ctx context.Context, name string) error { return ErrUnavailable }

func SetOff(name string, off bool) error { return ErrUnavailable }

func Prefer(name, id string) error { return ErrUnavailable }

func SetOptions(name string, opts map[string]any) error { return ErrUnavailable }

func ShortName(pkg string) string { var zero0 string; return zero0 }

func Version(spec string) string { var zero0 string; return zero0 }

func PackageName(spec string) string { var zero0 string; return zero0 }
func Tools(ctx context.Context) ([]PluginTools, error) {
	var zero0 []PluginTools
	var zero1 error
	return zero0, zero1
}

func RunTool(ctx context.Context, spec, name string, args json.RawMessage, agent string) (string, error) {
	var zero0 string
	return zero0, ErrUnavailable
}

func ReadList() (List, error) { var zero0 List; var zero1 error; return zero0, zero1 }

func ToolsOn() (int, error) { var zero0 int; var zero1 error; return zero0, zero1 }

func ToolNames() map[string][]string { var zero0 map[string][]string; return zero0 }

func Pinned(spec string) bool { var zero0 bool; return zero0 }
