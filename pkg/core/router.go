package core

import (
	"strings" // 字符串处理工具包

	"gnet-proxy/pkg/config"
)

/**
 * 🗺️ [路由核心：指挥官]
 * Router 就像是一个交通警察，手里拿着地图（配置），
 * 负责告诉每一辆来访的车辆（连接）该往哪条路（后端）走。
 */
type Router struct {
	routes map[string]config.RouteRule // 存储所有配置好的路由规则
}

// NewRouter：构造函数
func NewRouter(routes map[string]config.RouteRule) *Router {
	return &Router{routes: routes}
}

/**
 * 🎯 Match：路由匹配算法。
 * 它可以根据“域名”或“客户端 IP”来决定由哪个后端接手。
 */
func (r *Router) Match(sni string, remoteAddr string) (config.RouteRule, bool) {
	// 🏠 1. 精准匹配 (Exact Match)
	// 如果配置里的名字正好跟 SNI 域名一模一样，直接中奖！
	if rule, ok := r.routes[sni]; ok {
		return rule, true
	}

	// 🕵️‍♀️ 2. 模式匹配 (Pattern Match)
	// 这里目前支持两种非常实用的“模糊匹配”：
	for name, rule := range r.routes {
		// A. 前缀匹配 (如 "google.*")
		if strings.HasSuffix(name, "*") {
			prefix := strings.TrimSuffix(name, "*")
			if strings.HasPrefix(sni, prefix) {
				return rule, true
			}
		}

		// B. 后缀匹配 (如 "*.example.com")
		if strings.HasPrefix(name, "*") {
			suffix := strings.TrimPrefix(name, "*")
			if strings.HasSuffix(sni, suffix) {
				return rule, true
			}
		}
	}

	// 🛡️ 3. 兜底策略 (Fallback)
	// 如果配置里定义了一个名叫 "fallback" 的路由，那么没匹配上的流量都会流向这里。
	if rule, ok := r.routes["fallback"]; ok {
		return rule, true
	}

	// ❌ 如果都没匹配上，只能返回匹配失败。
	return config.RouteRule{}, false
}
