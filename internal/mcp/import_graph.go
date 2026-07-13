package mcp

type importGraph struct {
	imports      map[string][]string // file -> imported files (rel)
	importers    map[string][]string // file -> importing files (rel)
	references   map[string][]string // file -> semantically referenced files (rel)
	referencedBy map[string][]string // file -> files with semantic references to it (rel)
	autoloads    map[string][]string // manifest -> Composer files-autoloaded files (rel)
	autoloadedBy map[string][]string // file -> Composer manifests autoloading it (rel)
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
