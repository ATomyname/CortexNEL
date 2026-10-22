// Package service 提供局域网可用 IPv4 地址嗅探与排序支持。
package service

import (
	"net"
	"strings"
)

// GetLocalIPv4s 返回本机所有可用的非回环 IPv4 地址列表（按常用局域网网段优先级排序）。
func GetLocalIPv4s() []string {
	var ips []string
	interfaces, err := net.Interfaces()
	if err != nil {
		return ips
	}

	for _, iface := range interfaces {
		// 忽略未启用或回环网卡
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip4.String())
			}
		}
	}

	sortLANIPs(ips)
	return ips
}

func sortLANIPs(ips []string) {
	for i := 0; i < len(ips); i++ {
		// 优先推荐 192.168.x.x
		if strings.HasPrefix(ips[i], "192.168.") {
			if i > 0 {
				ips[0], ips[i] = ips[i], ips[0]
			}
			return
		}
	}
	for i := 0; i < len(ips); i++ {
		// 其次推荐 10.x.x.x
		if strings.HasPrefix(ips[i], "10.") {
			if i > 0 {
				ips[0], ips[i] = ips[i], ips[0]
			}
			return
		}
	}
}

// GetPrimaryLANIP 获取首选局域网 IP（优先 192.168.* 或 10.*）。若未发现则返回 127.0.0.1。
func GetPrimaryLANIP() string {
	ips := GetLocalIPv4s()
	if len(ips) > 0 {
		return ips[0]
	}
	return "127.0.0.1"
}
