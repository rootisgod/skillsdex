package skillsdex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
)

func requireSafeBootstrap(ctx context.Context, db *sql.DB, addr string) error {
	active, err := defaultAdminPasswordActive(ctx, db)
	if err != nil {
		return err
	}
	if active && isPublicBind(addr) {
		return errors.New("refusing to serve on a public address while the default admin/admin password is active; bind to 127.0.0.1 for first setup or run `skillsdex admin reset-password` locally")
	}
	return nil
}

func isPublicBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return !ip.IsLoopback()
}

func parseTrustedProxies(raw []string) ([]*net.IPNet, error) {
	var proxies []*net.IPNet
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, "/") {
				ip := net.ParseIP(part)
				if ip == nil {
					return nil, fmt.Errorf("invalid trusted proxy %q", part)
				}
				if ip.To4() != nil {
					part += "/32"
				} else {
					part += "/128"
				}
			}
			_, network, err := net.ParseCIDR(part)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q", part)
			}
			proxies = append(proxies, network)
		}
	}
	return proxies, nil
}
