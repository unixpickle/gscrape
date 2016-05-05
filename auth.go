package gscrape

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"github.com/yhat/scrape"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	niceUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.11; rv:40.0) Gecko/20100101 " +
		"Firefox/40.0"
)

// A Session facilitates a connection to an authenticated Google service.
type Session struct {
	http.Client
}

// NewSession creates a fresh, unauthenticated session.
func NewSession() *Session {
	jar, _ := cookiejar.New(nil)
	return &Session{http.Client{Jar: jar}}
}

// Auth attempts to access a given URL, then enters the given
// credentials when the URL redirects to a login page.
func (s *Session) Auth(serviceURL, email, password string) error {
	resp, err := s.Get(serviceURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	parsed, err := html.ParseFragment(resp.Body, nil)
	if err != nil || len(parsed) == 0 {
		return err
	}
	root := parsed[0]
	form, ok := scrape.Find(root, scrape.ById("gaia_loginform"))
	if !ok {
		return errors.New("failed to process login page")
	}
	submission := url.Values{}
	for _, input := range scrape.FindAll(form, scrape.ByTag(atom.Input)) {
		submission.Add(getAttribute(input, "name"), getAttribute(input, "value"))
	}
	submission["Email"] = []string{email}
	submission["Passwd"] = []string{password}

	postResp, err := s.PostForm(resp.Request.URL.String(), submission)
	if err != nil {
		return err
	}
	postResp.Body.Close()

	if postResp.Request.Method == "POST" {
		return errors.New("login incorrect")
	}

	return nil
}

func (s *Session) Logout() error {
	resp, err := s.Get("https://accounts.google.com/Logout")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func getAttribute(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
