package gscrape

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
)

var niceUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.11; rv:40.0) Gecko/20100101 " +
	"Firefox/40.0"

type BookSource int

const (
	BookSourcePreordered BookSource = iota
	BookSourcePreviouslyRented
	BookSourcePublicDomain
	BookSourcePurchased
	BookSourceRented
	BookSourceSample
	BookSourceUploaded
)

var AllBookSources = []BookSource{
	BookSourcePreordered,
	BookSourcePreviouslyRented,
	BookSourcePublicDomain,
	BookSourcePurchased,
	BookSourceRented,
	BookSourceSample,
	BookSourceUploaded,
}

type BookInfo struct {
	Title   string   `json:"title"`
	Authors []string `json:"authors"`
}

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

type volumeObject struct {
	VolumeInfo BookInfo `json:"volumeInfo"`
}

type booksResponse struct {
	Items      []volumeObject `json:"items"`
	TotalItems int            `json:"totalItems"`
}

// MyBooks fetches the user's books, filtering by source.
//
// You must read the returned book channel all the way to the end,
// even if you are not interested in all the books.
//
// If an error occurs asynchronously, it will be sent on the error channel.
//
// Both returned channels will be closed once the asynchronous operation is complete.
func (p *PlayBooks) MyBooks(sources []BookSource) (<-chan BookInfo, <-chan error) {
	u := "https://clients6.google.com/books/v1/volumes/mybooks?" +
		"maxResults=40&source=ge-books-fe&key=" + p.info.requestKey
	for _, source := range sources {
		u += "&acquireMethod="
		switch source {
		case BookSourcePreordered:
			u += "PREORDERED"
		case BookSourcePreviouslyRented:
			u += "PREVIOUSLY_RENTED"
		case BookSourcePublicDomain:
			u += "PUBLIC_DOMAIN"
		case BookSourcePurchased:
			u += "PURCHASED"
		case BookSourceRented:
			u += "RENTED"
		case BookSourceSample:
			u += "SAMPLE"
		case BookSourceUploaded:
			u += "UPLOADED"
		default:
			panic("unknown book source: " + strconv.Itoa(int(source)))
		}
	}

	errChan := make(chan error, 1)
	bookChan := make(chan BookInfo)

	go func() {
		defer close(bookChan)
		defer close(errChan)

		i := 0
		for {
			useURL := u
			useURL += "&startIndex=" + strconv.Itoa(i)
			req, _ := http.NewRequest("GET", useURL, nil)
			req.Header.Add("Host", "clients6.google.com")
			req.Header.Add("OriginToken", p.info.originToken)
			req.Header.Add("X-Origin", "https://play.google.com")
			resp, err := p.s.Do(req)
			if err != nil {
				errChan <- err
				return
			}
			contents, err := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			var fullResponse booksResponse
			if err := json.Unmarshal(contents, &fullResponse); err != nil {
				errChan <- err
				return
			}
			for _, volume := range fullResponse.Items {
				bookChan <- volume.VolumeInfo
			}
			i += len(fullResponse.Items)
			if i >= fullResponse.TotalItems {
				return
			}
		}
	}()

	return bookChan, errChan
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
