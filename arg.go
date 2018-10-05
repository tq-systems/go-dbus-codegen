package dbusgen

import (
	"regexp"
	"strconv"
	"strings"
)

var varRegexp = regexp.MustCompile("_([a-zA-Z0-9])")

func newArg(identifier, signature string, prefix string, i int) arg {
	var name string
	if identifier == "" {
		name = prefix + strconv.Itoa(i)
	} else if isKeyword(identifier) {
		name = prefix + strings.Title(identifier)
	} else {
		name = strings.ToLower(identifier[:1]) +
			varRegexp.ReplaceAllStringFunc(identifier[1:], func(s string) string {
				return strings.Title(s[1:])
			})
	}
	return arg{name, newSig(signature)[0]}
}

type arg struct {
	name string
	kind string
}

var keywords = []string{
	"break", "default", "func", "interface", "select",
	"case", "defer", "go", "map", "struct",
	"chan", "else", "goto", "package", "switch",
	"const", "fallthrough", "if", "range", "type",
	"continue", "for", "import", "return", "var",
}

func isKeyword(s string) bool {
	for i := 0; i < len(keywords); i++ {
		if s == keywords[i] {
			return true
		}
	}
	return false
}
