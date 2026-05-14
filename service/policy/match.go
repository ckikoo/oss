package policy

import (
	"net"
	"path"
	"time"

	"oss/service/do"
)

func matchPrincipal(principals []*do.PolicyPrincipalDo, whos []string) bool {
	for _, p := range principals {
		if p.Value == "*" {
			return true
		}
		full := p.Type + ":" + p.Value
		for _, who := range whos {
			if full == who {
				return true
			}
		}
	}
	return false
}

func matchAction(actions []string, action string) bool {
	for _, a := range actions {
		if a == "*" || a == action {
			return true
		}
		if matched, _ := path.Match(a, action); matched {
			return true
		}
	}
	return false
}

func matchResource(resources []string, resource string) bool {
	for _, r := range resources {
		if r == "*" || r == resource {
			return true
		}
		if matched, _ := path.Match(r, resource); matched {
			return true
		}
	}
	return false
}

func matchConditions(conds []*do.PolicyConditionDo, req do.EvaluateReq) bool {
	for _, cond := range conds {
		switch cond.Type {

		case "IpAddress":
			_, ipNet, err := net.ParseCIDR(cond.Value)
			if err != nil {
				return false
			}
			ip := net.ParseIP(req.SourceIP)
			if ip == nil || !ipNet.Contains(ip) {
				return false
			}

		case "NotIpAddress":
			_, ipNet, err := net.ParseCIDR(cond.Value)
			if err != nil {
				return false
			}
			ip := net.ParseIP(req.SourceIP)
			if ip != nil && ipNet.Contains(ip) {
				return false
			}

		case "TimeRange":
			now := time.Now().Format("15:04")
			switch cond.CondKey {
			case "start":
				if now < cond.Value {
					return false
				}
			case "end":
				if now > cond.Value {
					return false
				}
			}
		}
	}
	return true
}
