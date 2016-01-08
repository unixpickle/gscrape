package gscrape

import (
	"errors"
	"io/ioutil"
	"net/http"
	"regexp"
)

var niceUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.11; rv:40.0) Gecko/20100101 " +
	"Firefox/40.0"

// AuthPlayBooks is a wrapper for Authenticate() that uses Google Play Books.
func (s *Session) AuthPlayBooks(email, password string) error {
	return s.Auth("https://play.google.com/books", email, password)
}

type PlayBooks struct {
	s    *Session
	info playBooksAuthInfo
}

func NewPlayBooks(s *Session) (*PlayBooks, error) {
	info, err := s.getPlayBooksAuthInfo()
	if err != nil {
		return nil, err
	}
	return &PlayBooks{s, *info}, nil
}

// MyBooks fetches the raw "mybooks" API.
// TODO: support pagination, parsing, and filter options.
func (p *PlayBooks) MyBooks() (string, error) {
	u := "https://clients6.google.com/books/v1/volumes/mybooks?" +
		"acquireMethod=PREORDERED&acquireMethod=PREVIOUSLY_RENTED&acquireMethod=PUBLIC_DOMAIN&" +
		"acquireMethod=PURCHASED&acquireMethod=RENTED&acquireMethod=SAMPLE&" +
		"acquireMethod=UPLOADED&maxResults=40&source=ge-books-fe&startIndex=0&key=" +
		p.info.requestKey
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Add("Host", "clients6.google.com")
	req.Header.Add("OriginToken", p.info.originToken)
	req.Header.Add("X-Origin", "https://play.google.com")
	resp, err := p.s.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(contents), nil
}

// playBooksAuthInfo stores extra authentication information needed
// to make Play Books web API requests.
type playBooksAuthInfo struct {
	requestKey  string
	originToken string
}

func (s *Session) getPlayBooksAuthInfo() (*playBooksAuthInfo, error) {
	req, _ := http.NewRequest("GET", "https://play.google.com/books", nil)

	// Setting a user-agent is necessary; otherwise we do not get the expected JS.
	req.Header.Set("User-Agent", niceUserAgent)
	resp, err := s.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	re1 := regexp.MustCompile("var js_flags=\\[\"(.*?)\"")
	res := re1.FindStringSubmatch(string(contents))
	if len(res) != 2 {
		return nil, errors.New("failed to extract key from homepage")
	}
	key := res[1]
	re2 := regexp.MustCompile("remove\",\"\"\\]\\],\"(.*?)\"")
	res = re2.FindStringSubmatch(string(contents))

	if len(res) != 2 {
		return nil, errors.New("failed to extract origin token from homepage")
	}
	return &playBooksAuthInfo{key, res[1]}, nil
}
