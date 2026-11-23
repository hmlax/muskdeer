// Package muskdeer defines the independent administration module catalog.
package muskdeer

// Management catalog identifiers remain stable across repository relocation.
const (
	Module                       = "muskdeer"
	ServiceTag                   = "MuskdeerService"
	RootName                     = "Muskdeer"
	RootPath                     = "/muskdeer"
	RetiredSyntheticMenuName     = "MuskdeerSynthetic"
	RetiredOverviewMenuName      = "MuskdeerOverview"
	OverviewPermissionCode       = "muskdeer:overview:view"
	OverviewDashboardMenuName    = "Dashboard"
	OverviewAnalyticsMenuName    = "Analytics"
	AccountCreatePermissionCode  = "muskdeer:account:create"
	AccountEnablePermissionCode  = "muskdeer:account:enable"
	accountEnablePermissionTitle = "启用交易账户"
	accountCreatePermissionTitle = "添加交易账户"
)

// Page describes one portal page.
type Page struct {
	Path         string
	Resource     string
	Title        string
	Name         string
	PlatformOnly bool
	MerchantOnly bool
	Hidden       bool
}

// Pages returns the management page catalog.
func Pages() []Page {
	return []Page{
		{
			Path:         "gateways",
			Resource:     "gateway",
			Title:        "网关",
			Name:         "MuskdeerGateways",
			PlatformOnly: true,
		},
		{
			Path:         "overview",
			Resource:     "overview",
			Title:        "总览",
			Name:         RetiredOverviewMenuName,
			MerchantOnly: true,
			Hidden:       true,
		},
		{
			Path:         "templates",
			Resource:     "template",
			Title:        "策略模板",
			Name:         "MuskdeerTemplates",
			PlatformOnly: true,
		},
		{
			Path:         "instances",
			Resource:     "instance",
			Title:        "策略实例",
			Name:         "MuskdeerInstances",
			MerchantOnly: true,
		},
		{
			Path:         "instruments",
			Resource:     "instrument",
			Title:        "品种",
			Name:         "MuskdeerInstruments",
			PlatformOnly: true,
		},
		{
			Path:         "accounts",
			Resource:     "account",
			Title:        "账户额度",
			Name:         "MuskdeerAccounts",
			MerchantOnly: true,
		},
		{
			Path:         "orders",
			Resource:     "order",
			Title:        "订单记录",
			Name:         "MuskdeerOrders",
			MerchantOnly: true,
		},
		{
			Path:         "cancellations",
			Resource:     "cancellation",
			Title:        "撤单记录",
			Name:         "MuskdeerCancellations",
			MerchantOnly: true,
		},
		{
			Path:         "synthetic",
			Resource:     "synthetic",
			Title:        "开发成交",
			Name:         RetiredSyntheticMenuName,
			MerchantOnly: true,
			Hidden:       true,
		},
		{
			Path:         "snapshot",
			Resource:     "snapshot",
			Title:        "挂单与持仓",
			Name:         "MuskdeerSnapshot",
			MerchantOnly: true,
		},
		{
			Path:         "account-bindings",
			Resource:     "binding",
			Title:        "商户账户绑定",
			Name:         "MuskdeerBindings",
			PlatformOnly: true,
			Hidden:       true,
		},
	}
}

// Capability describes one independently authorized action.
type Capability struct {
	Code         string
	Title        string
	Page         string
	Methods      []string
	PlatformOnly bool
	MerchantOnly bool
}

// Allows reports whether the resource belongs to the requested portal.
func (p Page) Allows(platform bool) bool {
	return !p.Hidden && (!p.PlatformOnly || platform) && (!p.MerchantOnly || !platform)
}

// Allows reports whether the resource belongs to the requested portal.
func (c Capability) Allows(platform bool) bool {
	return (!c.PlatformOnly || platform) && (!c.MerchantOnly || !platform)
}

// MethodAllowed also covers selector APIs shared by the two realms.
func MethodAllowed(method string, platform bool) bool {
	for _, capability := range Capabilities() {
		if !capability.Allows(platform) {
			continue
		}
		for _, candidate := range capability.Methods {
			if candidate == method {
				return true
			}
		}
	}
	return false
}

// MenuAllowed is enforced when returning navigation, including stale/custom grants.
func MenuAllowed(name string, platform bool) bool {
	for _, page := range Pages() {
		if name == page.Name {
			return page.Allows(platform)
		}
	}
	return true
}

// Capabilities returns the separately authorized API capabilities.
func Capabilities() []Capability {
	return []Capability{
		{
			Code:         AccountEnablePermissionCode,
			Title:        accountEnablePermissionTitle,
			Page:         "accounts",
			Methods:      []string{"EnableAccount"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:gateway:view",
			Title:        "查看网关",
			Page:         "gateways",
			Methods:      []string{"ListGateways"},
			PlatformOnly: true,
		},
		{
			Code:         "muskdeer:catalog:select",
			Title:        "选择策略模板和品种",
			Page:         "instances",
			Methods:      []string{"ListTemplates", "ListInstruments"},
			MerchantOnly: true,
		},
		{
			Code:         OverviewPermissionCode,
			Title:        "查看总览",
			Page:         "overview",
			Methods:      []string{"GetOverview"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:template:view",
			Title:        "查看策略模板",
			Page:         "templates",
			Methods:      []string{"ListTemplates"},
			PlatformOnly: true,
		},
		{
			Code:         "muskdeer:instance:view",
			Title:        "查看策略实例",
			Page:         "instances",
			Methods:      []string{"ListInstances", "GetInstance", "CheckInstance"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:instrument:view",
			Title:        "查看品种",
			Page:         "instruments",
			Methods:      []string{"ListInstruments"},
			PlatformOnly: true,
		},
		{
			Code:         "muskdeer:account:view",
			Title:        "查看账户额度",
			Page:         "accounts",
			Methods:      []string{"ListAccounts"},
			MerchantOnly: true,
		},
		{
			Code:         AccountCreatePermissionCode,
			Title:        accountCreatePermissionTitle,
			Page:         "accounts",
			Methods:      []string{"CreateAccount", "ListInstruments"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:order:view",
			Title:        "查看订单记录",
			Page:         "orders",
			Methods:      []string{"ListOrderRecords"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:cancellation:view",
			Title:        "查看撤单记录",
			Page:         "cancellations",
			Methods:      []string{"ListCancellations"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:synthetic:view",
			Title:        "查看开发成交",
			Page:         "synthetic",
			Methods:      []string{"ListSyntheticRecords"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:snapshot:view",
			Title:        "查看挂单与持仓",
			Page:         "snapshot",
			Methods:      []string{"GetAccountSnapshot"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:binding:view",
			Title:        "查看商户账户绑定",
			Page:         "",
			Methods:      []string{"ListAccountBindings"},
			PlatformOnly: true,
		},
		{
			Code:         "muskdeer:instance:create",
			Title:        "创建策略实例",
			Page:         "instances",
			Methods:      []string{"CreateInstance"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:instance:update",
			Title:        "修改实例参数",
			Page:         "instances",
			Methods:      []string{"UpdateInstance"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:instance:start",
			Title:        "启动实例",
			Page:         "instances",
			Methods:      []string{"StartInstance"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:instance:stop",
			Title:        "停止实例",
			Page:         "instances",
			Methods:      []string{"StopInstance"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:instance:cancel",
			Title:        "撤销实例挂单",
			Page:         "instances",
			Methods:      []string{"CancelInstanceOrders"},
			MerchantOnly: true,
		},
		{
			Code:         "muskdeer:binding:create",
			Title:        "绑定商户账户",
			Page:         "",
			Methods:      []string{"CreateAccountBinding"},
			PlatformOnly: true,
		},
		{
			Code:         "muskdeer:binding:update",
			Title:        "启用禁用绑定",
			Page:         "",
			Methods:      []string{"SetAccountBindingEnabled"},
			PlatformOnly: true,
		},
	}
}
