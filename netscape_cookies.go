package main

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

const (
	CookieDomain = iota
	CookieHostOnly
	CookiePath
	CookieSecure
	CookieExpiration
	CookieName
	CookieValue
	CookiePieces
)

func (di *DownloadInfo) ParseNetscapeCookiesFile(fname string) (*cookiejar.Jar, error) {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	cookieMap := make(map[string][]*http.Cookie)

	for scanner.Scan() {
		// Could move everything in this loop into its own function
		cookieParts := strings.Split(scanner.Text(), "\t")
		secure := false
		httpOnly := false
		var expire int64
		var domain string

		// Netscape cookie entries should always have 7 pieces to them
		if len(cookieParts) != CookiePieces {
			continue
		}

		// Google started setting a json cookie.
		// Technically speaking this is not considered valid as far as the RFC is concerned.
		// Just skip them to avoid dumb logging Go does because ???
		if strings.Contains(cookieParts[CookieValue], `"`) {
			continue
		}

		domain = strings.ToLower(cookieParts[CookieDomain])
		expire, _ = strconv.ParseInt(cookieParts[CookieExpiration], 10, 64)
		expireTime := time.Unix(expire, 0)

		if strings.HasPrefix(domain, "#httponly_") {
			httpOnly = true
			domain = strings.TrimPrefix(domain, "#httponly_")
		}

		if strings.ToLower(cookieParts[CookieSecure]) == "true" {
			secure = true
		}

		cookie := &http.Cookie{
			Domain:   domain,
			Path:     cookieParts[CookiePath],
			Secure:   secure,
			Expires:  expireTime,
			Name:     cookieParts[CookieName],
			Value:    cookieParts[CookieValue],
			HttpOnly: httpOnly,
		}

		if _, ok := cookieMap[domain]; !ok {
			cookieMap[domain] = make([]*http.Cookie, 0)
		}

		cookieMap[domain] = append(cookieMap[domain], cookie)
	}

	if len(cookieMap) > 0 {
		for _, cookies := range cookieMap {
			url, err := url.Parse(fmt.Sprintf("https://%s", cookies[0].Domain))

			if err == nil {
				jar.SetCookies(url, cookies)
				if strings.HasSuffix(url.Host, "youtube.com") {
					di.CookiesURL = url
				}
			}
		}
	}

	return jar, nil
}
