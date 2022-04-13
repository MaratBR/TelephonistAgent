package main

import (
	"bufio"
	"os"
	"strings"

	"github.com/MaratBR/TelephonistAgent/locales"
)

func contains[T comparable](arr []T, v T) bool {
	for _, item := range arr {
		if item == v {
			return true
		}
	}
	return false
}

func addToSet[T comparable](arr []T, v T) []T {
	if contains[T](arr, v) {
		return arr
	}
	return append(arr, v)
}

func readYn() bool {
	reader := bufio.NewReader(os.Stdin)

	yes := strings.Split(locales.M.Y, ",")
	no := strings.Split(locales.M.N, ",")

	for {
		print(locales.M.Yn)

		text, _ := reader.ReadString('\n')
		text = text[:len(text)-1]
		for _, v := range no {
			if v == text {
				return false
			}
		}
		for _, v := range yes {
			if v == text {
				return true
			}
		}
	}
}

func readString(title string) string {
	reader := bufio.NewReader(os.Stdin)
	print(title)
	print(">")
	value, err := reader.ReadString('\n')
	if err != nil {
		panic(err)
	}
	value = value[:len(value)-1]
	return value
}

func readStringWithCondition(title string, condition func(string) bool) string {
	reader := bufio.NewReader(os.Stdin)
	valid := false
	var value string
	for !valid {
		print(title)
		print(">")
		value, _ = reader.ReadString('\n')
		valid = condition(value)
	}
	return value
}

func isNotEmptyString(s string) bool {
	return strings.Trim(s, " \t\n") != ""
}

func readTags(title string) []string {
	tags := []string{}
	str := readString(title)
	parts := strings.Split(str, ",")
	for _, part := range parts {
		part = strings.Trim(part, " \t")
		if part != "" {
			tags = addToSet(tags, part)
		}
	}
	return tags
}
