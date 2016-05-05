package gscrape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/PuerkitoBio/goquery"
)

type Video struct {
	Title       string
	Description string
	Author      string
	Id          string
	Thumbnail   string
	Length      string
}

// Returns watch history.
// In case of an error it returns the gathered videos up to that point.
// The time it takes to get the history depends on the number of watched videos.
// It might take a long time. I warned you.
func (s *Session) YTWatchHistory(email, password string) ([]Video, error) {
	if err := s.Auth("https://accounts.google.com/ServiceLogin?service=youtube",
		email, password); err != nil {
		return nil, err
	}
	{ // this is required to get some cookies
		req, _ := http.NewRequest("GET",
			"https://www.youtube.com/feed/subscriptions", nil)
		req.Header.Set("User-Agent", niceUserAgent)
		resp, err := s.Do(req)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
	}
	return s.getWatchHistory()
}

func (s *Session) getWatchHistory() ([]Video, error) {
	videos := make([]Video, 0, 128)
	getVideos := func(doc *goquery.Document) {
		doc.Find(".yt-lockup-video").Each(
			func(i int, s *goquery.Selection) {
				var video Video
				video.Id, _ = s.Attr("data-context-item-id")
				video.Thumbnail, _ = s.Find("img").Attr("src")
				video.Length = s.Find(".video-time").Text()
				video.Title, _ = s.Find(".yt-lockup-title a").Attr("title")
				video.Author = s.Find(".yt-lockup-byline a").Text()
				dn := s.Find(".yt-lockup-description")
				if dn.Length() > 0 {
					video.Description = dn.Text()
				}
				videos = append(videos, video)
			})
	}
	var more string
	var hasMore bool
	{ // the first few videos are directly on the page
		var doc *goquery.Document
		{
			req, err := http.NewRequest("GET",
				"https://www.youtube.com/feed/history", nil)
			req.Header.Set("User-Agent", niceUserAgent)
			resp, err := s.Do(req)
			if err != nil {
				return videos, err
			}
			doc, err = goquery.NewDocumentFromResponse(resp)
			if err != nil {
				resp.Body.Close()
				return videos, err
			}
			resp.Body.Close()
		}
		getVideos(doc)
		mn := doc.Find("[data-uix-load-more-href]")
		more, hasMore = mn.Attr("data-uix-load-more-href")
	}
	// all the other videos need to be gathered through ajax loaded json
	for hasMore {
		var yth struct {
			ContentHTML        string `json:"content_html"`
			LoadMoreWidgetHTML string `json:"load_more_widget_html"`
		}
		{
			url_str := fmt.Sprint("https://www.youtube.com", more)
			req, err := http.NewRequest("GET", url_str, nil)
			req.Header.Set("User-Agent", niceUserAgent)
			resp, err := s.Do(req)
			if err != nil {
				return videos, err
			}
			if err := json.NewDecoder(resp.Body).Decode(&yth); err != nil {
				resp.Body.Close()
				return videos, err
			}
			resp.Body.Close()
		}
		{
			doc, err := goquery.NewDocumentFromReader(
				bytes.NewBufferString(yth.ContentHTML))
			if err != nil {
				return videos, err
			}
			getVideos(doc)
		}
		{
			doc, err := goquery.NewDocumentFromReader(
				bytes.NewBufferString(yth.LoadMoreWidgetHTML))
			if err != nil {
				return videos, err
			}
			mn := doc.Find("[data-uix-load-more-href]")
			more, hasMore = mn.Attr("data-uix-load-more-href")
		}
	}
	return videos, nil
}
