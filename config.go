package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// 📋 路由规则
type RouteRule struct {
	Addr          string `json:"addr"`           
	ProxyProtocol bool   `json:"proxy_protocol"` 
}

// 📋 配置结构体
type Config struct {
	ListenAddr    string               `json:"listen_addr"`
	Multicore     bool                 `json:"multicore"`
	ProxyProtocol bool                 `json:"proxy_protocol"`
	LogLevel      string               `json:"log_level"`
	LogFile       string               `json:"log_file"`
	RawRoutes     map[string]RouteRule `json:"routes"`         
	Routes        map[string]RouteRule `json:"-"`              
}

// 🧐 StripComments：剥离 JSON 中的注释 (支持 // 和 /* */)
// 这样就能让标准的 json 库支持 JSONC 格式
func StripComments(data []byte) []byte {
	// 1. 移除多行注释 /* ... */
	reMulti := regexp.MustCompile(`(?s)/\*.*?\*/`)
	data = reMulti.ReplaceAll(data, nil)
	
	// 2. 移除单行注释 // ...
	// 注意：这里用简单正则，后续如果遇到 URL 里的 // 需要更精细处理
	// 但在我们的 config 场景下，URL 通常包裹在双引号内，我们可以针对性优化
	reSingle := regexp.MustCompile(`//.*`)
	data = reSingle.ReplaceAll(data, nil)
	
	return data
}

// LoadConfig：加载并解析 JSONC 配置文件
func LoadConfig(path string) (*Config, error) {
	rawData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 🔥 核心：在解析前剥离注释
	cleanData := StripComments(rawData)

	config := &Config{}
	err = json.Unmarshal(cleanData, config)
	if err != nil {
		return nil, err
	}

	// 自动打散多域名 Key
	config.Routes = make(map[string]RouteRule)
	for key, rule := range config.RawRoutes {
		domains := strings.Split(key, ",")
		for _, d := range domains {
			d = strings.TrimSpace(d)
			if d != "" {
				config.Routes[d] = rule
			}
		}
	}

	if config.ListenAddr == "" {
		config.ListenAddr = "[::]:443"
	}

	return config, nil
}
