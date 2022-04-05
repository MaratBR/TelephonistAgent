package locales

//go:generate $GOPATH/bin/gojson -input ./en.json -o generated_messages.go -pkg locales -name Messages

import (
	_ "embed"
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

var (
	//go:embed en.json
	en []byte

	//go:embed ru.json
	ru []byte
	M  *Messages
)

var langMap map[string][]byte = map[string][]byte{
	"EN": en,
	"RU": ru,
}

var languageRegexp = regexp.MustCompile(`^(.*)[-_]([^.]*)(\..*)?$`)

func normalizeLang(language string) string {
	if languageRegexp.MatchString(language) {
		return languageRegexp.FindStringSubmatch(language)[1]
	}
	return strings.ToUpper(language)
}

func getMessages(language string) *Messages {
	language = normalizeLang(language)
	var src []byte
	var ok bool
	if src, ok = langMap[language]; !ok {
		src = en
	}

	messages := new(Messages)
	err := json.Unmarshal(src, messages)
	if err != nil {
		panic(err)
	}
	return messages
}

func SetLanguage(language string) {
	M = getMessages(language)
}

func detectLanguage() string {
	lang, ok := os.LookupEnv("LANG")
	if ok {
		return lang
	}
	lang, ok = os.LookupEnv("LANG")
	if ok {
		return lang
	}
	return "EN"
}

func init() {
	SetLanguage(detectLanguage())
}
