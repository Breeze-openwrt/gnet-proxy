package main

import (
	"log"

	"github.com/panjf2000/gnet/v2"
)

func (s *proxyServer) OnBoot(eng gnet.Engine) gnet.Action {
	log.Printf("🚀 gnet-proxy 极速分流器启动成功！监听: %s", s.addr)
	log.Printf("🧠 多核模式: %v | 日志冗余级别: %d", s.multicore, s.verbosity)
	return gnet.None
}

func (s *proxyServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	s.infof("🔌 [接入] 新客户端: %s", c.RemoteAddr())
	return nil, gnet.None
}

func (s *proxyServer) OnTraffic(c gnet.Conn) gnet.Action {
	if c.Context() == nil {
		buf, _ := c.Peek(-1)

		// 极其关键：TLS 数据在开始传输前必然需要收集齐
		if len(buf) == 0 {
			return gnet.None
		}

		sni, err := ParseSNI(buf)
		if err != nil {
			if err == ErrIncompletePacket {
				// 握手包还没接收完，继续包组装
				return gnet.None
			}
			s.infof("❓ [无域名/非TLS流量] 客户端 %s 的流量未识别到 SNI (原因: %v)，将尝试 Fallback 回退路由", c.RemoteAddr(), err)
		} else {
			s.infof("🔍 [SNI 提取成功] 客户端 %s 识别到域名: %s", c.RemoteAddr(), sni)
		}

		// 路由匹配优先级：精准命中 > 星号 (*) Fallback 兜底
		rule, ok := s.routes[sni]
		if !ok {
			// 如果没精准命中，或是根本没提取到 SNI，尝试找万能回退路由
			fallbackRule, fallbackOk := s.routes["*"]
			if !fallbackOk {
				// 没有配置回退，只能拒绝
				s.infof("⚠️ [拒绝访问] 域名 [%s] 未匹配且无 (*) 回退路由，掐断客户端 %s", sni, c.RemoteAddr())
				return gnet.Close
			}
			rule = fallbackRule
			s.infof("🛡️ [启用 Fallback] 客户端 %s 未完全匹配，路由至兜底后端: %s", c.RemoteAddr(), rule.Addr)
		} else {
			s.infof("🎯 [路由精准命中] 客户端 %s 分流: [%s] -> %s", c.RemoteAddr(), sni, rule.Addr)
		}

		backendConn, err := s.dialBackend(rule)
		if err != nil {
			s.errorf("❌ [拨号失败] 无法连接到后端 %s (客户端 %s): %v", rule.Addr, c.RemoteAddr(), err)
			return gnet.Close
		}
		s.infof("✅ [拨号成功] 已连通后端 %s (客户端 %s)", rule.Addr, c.RemoteAddr())

		newCtx := &connContext{backendConn: backendConn, isProxying: true}
		c.SetContext(newCtx)
		go s.proxyBack(c, backendConn)
	}

	pCtx := c.Context().(*connContext)
	msg, _ := c.Next(-1)

	// 记录请求转发量（只有在 trace 级别，也就是 -vvv 时才大规模刷屏显示字节数，避免性能损耗）
	s.tracef("⬆️ [上行数据] (Client %s -> Backend) 转发了 %d 字节", c.RemoteAddr(), len(msg))

	_, err := pCtx.backendConn.Write(msg)
	if err != nil {
		s.errorf("❌ [转发异常] 发送数据到后端失败 (Client %s): %v", c.RemoteAddr(), err)
		return gnet.Close
	}
	return gnet.None
}

func (s *proxyServer) OnClose(c gnet.Conn, err error) gnet.Action {
	if err != nil {
		s.errorf("❌ [连接断开] 客户端异常断开 (Client %s): %v", c.RemoteAddr(), err)
	} else {
		s.infof("👋 [连接关闭] 客户端正常断开 (Client %s)", c.RemoteAddr())
	}

	if c.Context() != nil {
		pCtx := c.Context().(*connContext)
		if pCtx.backendConn != nil {
			s.debugf("🧹 [清理] 销毁与后端的连接 (Client %s)", c.RemoteAddr())
			pCtx.backendConn.Close()
		}
	}
	return gnet.None
}
