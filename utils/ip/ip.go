package ip

import (
	"fmt"
	"net"
	"net/url"
)

func ValidateCallbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}

	if u.Scheme != "https" {
		return fmt.Errorf("callback url must use https")
	}

	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("localhost callback is not allowed")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}

	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("private callback address is not allowed")
		}
	}

	return nil
}
