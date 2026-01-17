package mcp

type importGraph struct {
	imports   map[string][]string // file -> imported files (rel)
	importers map[string][]string // file -> importing files (rel)
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
