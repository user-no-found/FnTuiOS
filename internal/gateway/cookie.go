package gateway

import (
	"errors"
	"net/http"
)

// Only the documented Inbox item cookie crosses the NAS-to-user-service boundary.
func applicationCookie(request *http.Request) (*http.Cookie, error) {
	cookies := request.CookiesNamed("fntuios_inbox")
	if len(cookies) == 0 {
		return nil, nil
	}
	if len(cookies) != 1 || !validInboxID(cookies[0].Value) {
		return nil, errors.New("invalid app cookie")
	}
	return &http.Cookie{Name: "tuios_inbox", Value: cookies[0].Value}, nil
}

func validInboxID(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
