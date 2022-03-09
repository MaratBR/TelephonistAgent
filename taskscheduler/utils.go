package taskscheduler

import "strings"

func matchesPath(root, path string) bool {
	return strings.HasPrefix(path, root)
}

func normalizePath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")
	return path
}
