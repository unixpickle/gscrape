package gscrape

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yhat/scrape"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	shortDurationRegexp = regexp.MustCompile("^([0-9]*):([0-9]*)$")
	longDurationRegexp  = regexp.MustCompile("^([0-9]*):([0-9]*):([0-9]*)$")
)

// YoutubeVideoInfo stores various metadata about
// a Youtube video.
type YoutubeVideoInfo struct {
	Title        string
	Description  string
	Author       string
	ID           string
	ThumbnailURL *url.URL
	Length       time.Duration
}

// A Youtube object wraps a session and provides
// various youtube-related APIs.
type Youtube struct {
	s *Session
}

// AuthYoutube authenticates a youtube user using
// a session and returns a Youtube instance for
// using the youtube-related features of the session.
func (s *Session) AuthYoutube(email, password string) (*Youtube, error) {
	if err := s.Auth("https://accounts.google.com/ServiceLogin?service=youtube",
		"https://accounts.google.com/ServiceLoginAuth", email, password); err != nil {
		return nil, err
	}

	// Get some youtube-specific cookies.
	req, _ := http.NewRequest("GET", "https://www.youtube.com/feed/subscriptions", nil)
	req.Header.Set("User-Agent", spoofedUserAgent)
	resp, err := s.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	return &Youtube{s}, nil
}

// History asynchronously fetches the user's
// video viewing history.
// You may provide a cancel channel which you
// can close to cancel the fetch mid-way.
func (y *Youtube) History(cancel <-chan struct{}) (<-chan *YoutubeVideoInfo, <-chan error) {
	videoChan := make(chan *YoutubeVideoInfo)
	errChan := make(chan error, 1)

	go func() {
		defer close(videoChan)
		defer close(errChan)

		historyReq, _ := http.NewRequest("GET", "https://www.youtube.com/feed/history", nil)
		historyReq.Header.Set("User-Agent", spoofedUserAgent)
		resp, err := y.s.Do(historyReq)
		rootNode, err := html.Parse(resp.Body)
		resp.Body.Close()
		if err != nil {
			errChan <- err
			return
		}

		loadMoreHTML := rootNode
		contentHTML := rootNode
		for {
			items := parseHistoryItems(contentHTML)
			for _, item := range items {
				select {
				case videoChan <- item:
				case <-cancel:
					return
				}
			}

			if loadMoreHTML == nil {
				break
			}

			loadButton, ok := scrape.Find(loadMoreHTML, scrape.ByClass("yt-uix-load-more"))
			if ok {
				morePath := scrape.Attr(loadButton, "data-uix-load-more-href")
				loadMoreHTML, contentHTML, err = y.fetchMoreHistory(morePath)
				if err != nil {
					errChan <- err
					return
				}
			}
		}
	}()

	return videoChan, errChan
}

// FullHistory synchronously fetches the user's
// full video watching history.
// If an error occurs, this will return partial
// results and the first error encountered.
func (y *Youtube) FullHistory() ([]YoutubeVideoInfo, error) {
	var res []YoutubeVideoInfo

	vidChan, errChan := y.History(nil)

	for video := range vidChan {
		res = append(res, *video)
	}

	return res, <-errChan
}

func (y *Youtube) fetchMoreHistory(moreHref string) (more, content *html.Node, err error) {
	moreURL := "https://www.youtube.com" + moreHref
	moreReq, err := http.NewRequest("GET", moreURL, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := y.s.Do(moreReq)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var jsonDoc struct {
		Content string `json:"content_html"`
		More    string `json:"load_more_widget_html"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&jsonDoc); err != nil {
		return nil, nil, err
	}

	content, err = html.Parse(bytes.NewBufferString(jsonDoc.Content))
	if err != nil {
		return nil, nil, err
	}
	more, _ = html.Parse(bytes.NewBufferString(jsonDoc.More))

	return
}

func parseHistoryItems(rootNode *html.Node) []*YoutubeVideoInfo {
	videoElements := scrape.FindAll(rootNode, scrape.ByClass("yt-lockup-video"))

	res := make([]*YoutubeVideoInfo, len(videoElements))
	for i, element := range videoElements {
		res[i] = parseVideoInfo(element)
	}

	return res
}

func parseVideoInfo(element *html.Node) *YoutubeVideoInfo {
	var info YoutubeVideoInfo

	info.ID = scrape.Attr(element, "data-context-item-id")

	thumbnailContainer, ok := scrape.Find(element, scrape.ByClass("yt-thumb-simple"))
	if ok {
		thumbnailImage, ok := scrape.Find(thumbnailContainer, scrape.ByTag(atom.Img))
		if ok {
			thumb := scrape.Attr(thumbnailImage, "data-thumb")
			if thumb == "" {
				thumb = scrape.Attr(thumbnailImage, "src")
			}
			info.ThumbnailURL, _ = url.Parse(thumb)
		}
	}

	videoTimeElement, ok := scrape.Find(element, scrape.ByClass("video-time"))
	if ok {
		durationStr := strings.TrimSpace(scrape.Text(videoTimeElement))
		info.Length, _ = parseVideoDuration(durationStr)
	}

	linkFieldClasses := []string{"yt-lockup-title", "yt-lockup-byline"}
	linkFieldPtrs := []*string{&info.Title, &info.Author}
	for i, class := range linkFieldClasses {
		linkContainer, ok := scrape.Find(element, scrape.ByClass(class))
		if ok {
			link, ok := scrape.Find(linkContainer, scrape.ByTag(atom.A))
			if ok {
				*linkFieldPtrs[i] = strings.TrimSpace(scrape.Text(link))
			}
		}
	}

	descBox, ok := scrape.Find(element, scrape.ByClass("yt-lockup-description"))
	if ok {
		info.Description = strings.TrimSpace(scrape.Text(descBox))
	}

	return &info
}

func parseVideoDuration(duration string) (time.Duration, error) {
	shortParsed := shortDurationRegexp.FindStringSubmatch(duration)
	if shortParsed != nil {
		minutes, _ := strconv.Atoi(shortParsed[1])
		seconds, _ := strconv.Atoi(shortParsed[2])
		return time.Second * time.Duration(minutes*60+seconds), nil
	}
	longParsed := longDurationRegexp.FindStringSubmatch(duration)
	if longParsed != nil {
		hours, _ := strconv.Atoi(longParsed[1])
		minutes, _ := strconv.Atoi(longParsed[2])
		seconds, _ := strconv.Atoi(longParsed[3])
		return time.Second * time.Duration(hours*60*60+minutes*60+seconds), nil
	}
	return 0, errors.New("unable to parse duration: " + duration)
}
