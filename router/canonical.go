package router

import (
	"net/url"
	"sort"
	"strings"
)

func canonicalQueryWithEscaper(rawQuery string, escape func(string) string) string {
	if rawQuery == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0)
	for _, key := range keys {
		vals := values[key]
		sort.Strings(vals)

		for _, val := range vals {
			parts = append(parts, escape(key)+"="+escape(val))
		}
	}

	return strings.Join(parts, "&")
}
