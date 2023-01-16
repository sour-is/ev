package webfinger

import (
	"net/url"
	"strings"
)

type Addr struct {
	prefix []string
	URL    *url.URL
}

func Parse(s string) *Addr {
	addr := &Addr{}

	addr.URL, _ = url.Parse(s)

	if addr.URL.Opaque == "" {
		return addr
	}

	var hasPfx = true
	pfx := addr.URL.Scheme

	for hasPfx {
		addr.prefix = append(addr.prefix, pfx)
		pfx, addr.URL.Opaque, hasPfx = strings.Cut(addr.URL.Opaque, ":")
	}

	user, host, _ := strings.Cut(pfx, "@")
	addr.URL.User = url.User(user)
	addr.URL.Host = host

	return addr
}
