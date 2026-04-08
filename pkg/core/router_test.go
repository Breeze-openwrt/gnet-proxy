package core

import (
	"gnet-proxy/pkg/config"
	"testing"
)

// 🧪 单元测试：验证路由匹配算法的优先级（精准 > 通配符）
func TestRouterMatch(t *testing.T) {
	routes := map[string]config.RouteRule{
		"google.com": {
			Addr: "tcp://1.1.1.1:443",
		},
		"*.google.com": {
			Addr: "tcp://2.2.2.2:443",
		},
		"*": {
			Addr: "tcp://8.8.8.8:443",
		},
	}

	router := NewRouter(routes)

	tests := []struct {
		name     string
		sni      string
		wantAddr string
	}{
		{
			name:     "Exact Match",
			sni:      "google.com",
			wantAddr: "tcp://1.1.1.1:443",
		},
		{
			name:     "Subdomain Match",
			sni:      "mail.google.com",
			wantAddr: "tcp://2.2.2.2:443",
		},
		{
			name:     "Sub-subdomain Match",
			sni:      "test.mail.google.com",
			wantAddr: "tcp://2.2.2.2:443",
		},
		{
			name:     "Fallback Match",
			sni:      "bing.com",
			wantAddr: "tcp://8.8.8.8:443",
		},
		{
			name:     "Empty SNI Match (IP direct)",
			sni:      "",
			wantAddr: "tcp://8.8.8.8:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, ok := router.Match(tt.sni, "127.0.0.1")
			if !ok {
				t.Fatalf("Match failed for %s", tt.sni)
			}
			if rule.Addr != tt.wantAddr {
				t.Errorf("Match(%s) = %v, want %v", tt.sni, rule.Addr, tt.wantAddr)
			}
		})
	}
}
