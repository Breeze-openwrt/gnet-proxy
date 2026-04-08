package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tailscale/hujson"
)

// 📋 路由规则
type RouteRule struct {
	Addr        string `json:"addr"`
	JumpStart   int    `json:"jump_start"`   // 预热连接数
	IdleTimeout int    `json:"idle_timeout"` // 空闲超时（秒）
}

// 📋 日志配置 (对标 sing-box 格式)
type LogConfig struct {
	Disabled  bool   `json:"disabled"`
	Level     string `json:"level"`
	Output    string `json:"output"`
	Timestamp bool   `json:"timestamp"`
}

// 📋 配置结构体
type Config struct {
	ListenAddr string               `json:"listen_addr"`
	Multicore  bool                 `json:"multicore"`
	Log        LogConfig            `json:"log"`
	RawRoutes  map[string]RouteRule `json:"routes"`
	Routes     map[string]RouteRule `json:"-"`
}

// LoadConfig：加载并解析 JSONC 配置文件 (由业界领先的 tailscale/hujson 驱动)
func LoadConfig(path string) (*Config, error) {
	rawData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 🚀 工业级黑科技：通过 AST (抽象语法树) 完美处理注释和多余逗号
	cleanData, err := hujson.Standardize(rawData)
	if err != nil {
		return nil, fmt.Errorf("JSONC 标准化失败: %v", err)
	}

	config := &Config{}
	err = json.Unmarshal(cleanData, config)
	if err != nil {
		// 🚀 极致性能报错定位：如果是语法错误，计算行号和列号
		if jerr, ok := err.(*json.SyntaxError); ok {
			line, col := 1, 1
			for i := 0; i < int(jerr.Offset); i++ {
				if cleanData[i] == '\n' {
					line++
					col = 1
				} else {
					col++
				}
			}
			return nil, fmt.Errorf("配置文件配置错误 (发生位置: 第 %d 行, 第 %d 列): %v", line, col, jerr)
		}
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
