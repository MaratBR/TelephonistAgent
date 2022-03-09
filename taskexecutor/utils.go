package taskexecutor

import "strings"

func findShebang(script string) string {
	var shebang string

	lines := strings.Split(script, "\n")
	for _, line := range lines {
		line = strings.TrimLeft(line, " \t")
		if strings.HasPrefix(line, "#!") {
			shebang = strings.Trim(line[2:], " \t")
			if shebang != "" {
				break
			}
		}
	}

	return shebang
}

func getLines(data []byte) ([]string, []byte) {
	lines := []string{}
	isEmptyLine := true
	startIndex := 0
	stopIndex := -1

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			if !isEmptyLine {
				stopIndex = i
				lines = append(lines, string(data[startIndex:i]))
			}

			startIndex = i + 1
			isEmptyLine = true
			continue
		}
		if !(data[i] == ' ' || data[i] == '\t' || data[i] == '\r') && isEmptyLine {
			isEmptyLine = false
		}
	}

	var remaining []byte

	if stopIndex+1 >= len(data) {
		remaining = []byte{}
	} else {
		remaining = data[stopIndex+1:]
		isEmpty := true
		for i := 0; i < len(remaining); i++ {
			if remaining[i] != ' ' && remaining[i] != '\t' && remaining[i] != '\r' {
				isEmpty = false
				break
			}
		}
		if isEmpty {
			remaining = []byte{}
		}
	}

	return lines, remaining
}
