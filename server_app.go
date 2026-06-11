package skillsdex

import (
	"database/sql"
	"net"
	"net/http"
	"time"
)

type app struct {
	db                *sql.DB
	httpClient        httpDoer
	inviteEmailSender inviteEmailSender
	trustedProxies    []*net.IPNet
}

var defaultSourceHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (a *app) sourceHTTPClient() httpDoer {
	if a.httpClient != nil {
		return a.httpClient
	}
	return defaultSourceHTTPClient
}
