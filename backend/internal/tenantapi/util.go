package tenantapi

import "sort"

func sorted(s []string) []string {
	sort.Strings(s)
	return s
}
