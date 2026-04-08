package core

import (
	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/config"
)

// Router 定义了一个高内聚的路由分配器
type Router struct {
	routes map[string]config.RouteRule
}

// NewRouter 构建路由引警
func NewRouter(routes map[string]config.RouteRule) *Router {
	return &Router{routes: routes}
}

// Match 负责从 SNI 映射到具体的 RouteRule。集成了 Fallback 兜底逻辑。
func (r *Router) Match(sni string, clientIP string) (config.RouteRule, bool) {
	rule, ok := r.routes[sni]
	if !ok {
		fallbackRule, fallbackOk := r.routes["*"]
		if !fallbackOk {
			logger.Infof("⚠️ [拒绝访问] 域名 [%s] 未匹配且无 (*) 回退路由，掐断客户端 %s", sni, clientIP)
			return config.RouteRule{}, false
		}
		rule = fallbackRule
		logger.Infof("🛡️ [启用 Fallback] 客户端 %s 未完全匹配，路由至兜底后端: %s", clientIP, rule.Addr)
	} else {
		logger.Infof("🎯 [路由精准命中] 客户端 %s 分流: [%s] -> %s", clientIP, sni, rule.Addr)
	}
	return rule, true
}
