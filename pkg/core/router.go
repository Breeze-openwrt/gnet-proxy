package core

import (
	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/config"
	"strings"
)

// Router 定义了一个高内聚的路由分配器
type Router struct {
	routes map[string]config.RouteRule
}

// NewRouter 构建路由引警
func NewRouter(routes map[string]config.RouteRule) *Router {
	return &Router{routes: routes}
}

// Match 负责从 SNI 映射到具体的 RouteRule。集成了域名树逐级匹配与 Fallback 兜底逻辑。
func (r *Router) Match(sni string, clientIP string) (config.RouteRule, bool) {
	// 1. 尝试精准匹配
	if rule, ok := r.routes[sni]; ok {
		logger.Infof("🎯 [路由精准命中] 客户端 %s 分流: [%s] -> %s", clientIP, sni, rule.Addr)
		return rule, true
	}

	// 2. 尝试通配符逐级匹配 (例如 mail.google.com -> *.google.com -> *.com)
	labels := strings.Split(sni, ".")
	for i := 1; i < len(labels); i++ {
		wildcard := "*." + strings.Join(labels[i:], ".")
		if rule, ok := r.routes[wildcard]; ok {
			logger.Infof("🎭 [通配符命中] 客户端 %s 分流: [%s] -> %s (规则: %s)", clientIP, sni, rule.Addr, wildcard)
			return rule, true
		}
	}

	// 3. 最终兜底路由 (*)
	if fallbackRule, ok := r.routes["*"]; ok {
		logger.Infof("🛡️ [启用 Fallback] 客户端 %s 未完全匹配，路由至兜底后端: %s", clientIP, fallbackRule.Addr)
		return fallbackRule, true
	}

	logger.Infof("⚠️ [拒绝访问] 域名 [%s] 未匹配且无 (*) 回退路由，掐断客户端 %s", sni, clientIP)
	return config.RouteRule{}, false
}
