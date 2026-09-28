package ui

import (
	"net/http"
	"strings"
)

type Locale string

const (
	Chinese Locale = "zh-CN"
	English Locale = "en"
)

func requestLocale(r *http.Request) Locale {
	if cookie, err := r.Cookie("diskord_lang"); err == nil {
		if cookie.Value == string(English) {
			return English
		}
		if cookie.Value == string(Chinese) {
			return Chinese
		}
	}
	for _, item := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		language := strings.ToLower(strings.TrimSpace(strings.Split(item, ";")[0]))
		if strings.HasPrefix(language, "zh") {
			return Chinese
		}
		if strings.HasPrefix(language, "en") {
			return English
		}
	}
	return Chinese
}
