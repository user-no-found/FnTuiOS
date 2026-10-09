package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplicationCookieBoundary(t *testing.T) {
	request := validRequest()
	request.Header.Set("Cookie", "NAS_LOGIN=secret; fntuios_inbox=123")
	cookie, err := applicationCookie(request)
	if err != nil || cookie == nil || cookie.Name != "tuios_inbox" || cookie.Value != "123" {
		t.Fatal("scoped app cookie was not preserved")
	}
	request.Header.Add("Cookie", "fntuios_inbox=999")
	if _, err := applicationCookie(request); err == nil {
		t.Fatal("duplicate app cookies accepted")
	}
	request.Header.Set("Cookie", "fntuios_inbox=../../secret")
	if _, err := applicationCookie(request); err == nil {
		t.Fatal("invalid app cookie accepted")
	}
	request.Header.Set("Cookie", "NAS_LOGIN=secret")
	if cookie, err := applicationCookie(request); err != nil || cookie != nil {
		t.Fatal("NAS cookie crossed boundary")
	}
	response := httptest.NewRecorder()
	http.SetCookie(response, &http.Cookie{Name: "tuios_inbox", Value: "123", Path: "/", HttpOnly: true})
	http.SetCookie(response, &http.Cookie{Name: "NAS_LOGIN", Value: "replacement", Path: "/"})
	result := response.Result()
	if err := rewriteResponse(result, "/app/tuios", "127.0.0.1:1234"); err != nil {
		t.Fatal(err)
	}
	cookies := result.Header.Values("Set-Cookie")
	if len(cookies) != 1 || !strings.Contains(cookies[0], "fntuios_inbox=123") || !strings.Contains(cookies[0], "Path=/app/tuios/") {
		t.Fatalf("unsafe app cookie namespace: %v", cookies)
	}
}

func validRequest() *http.Request {
	r := httptest.NewRequest("GET", "https://nas.example/app/tuios/ws", nil)
	r.Header.Set("X-Trim-Userid", "1000")
	r.Header.Set("X-Trim-Username", "sun")
	r.Header.Set("Origin", "https://nas.example")
	return r
}
