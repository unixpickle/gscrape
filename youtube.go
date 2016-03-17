package gscrape

import (
	"net/http"

	"github.com/yhat/scrape"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Video struct {
	Title       string
	Description string
	Author      string
	Id          string
	Thumbnail   string
	Length      string
}

type YTWatchHistory struct {
	Videos []Video
}

func (s *Session) YTWatchHistory(email, password string) (*YTWatchHistory, error) {
	if err := s.Auth("https://accounts.google.com/ServiceLogin?service=youtube", email, password); err != nil {
		return nil, err
	}
	return s.getWatchHistory()
}

func (s *Session) getWatchHistory() (*YTWatchHistory, error) {
	req, _ := http.NewRequest("GET", "https://www.youtube.com/feed/subscriptions", nil)
	req.Header.Set("User-Agent", niceUserAgent)
	resp, err := s.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	videos := make([]Video, 0, 100)
	{ // first page is in response body
		parsed, err := html.Parse(resp.Body)
		if err != nil {
			return nil, err
		}
		videoNodes := scrape.FindAll(parsed, scrape.ByClass("yt-lockup-video"))
		for _, videoNode := range videoNodes {
			var video Video
			video.Id = getAttribute(videoNode, "data-context-item-id")
			thumbNode, _ := scrape.Find(videoNode, scrape.ByTag(atom.Img))
			video.Thumbnail = getAttribute(thumbNode, "src")
			lengthNode, _ := scrape.Find(videoNode, scrape.ByClass("video-time"))
			video.Length = lengthNode.FirstChild.Data
			titleNode, _ := scrape.Find(videoNode, scrape.ByClass("yt-lockup-title"))
			linkNode, _ := scrape.Find(titleNode, scrape.ByTag(atom.A))
			video.Title = getAttribute(linkNode, "title")
			descriptionNode, _ := scrape.Find(videoNode, scrape.ByClass("yt-lockup-description"))
			if descriptionNode != nil {
				video.Description = descriptionNode.FirstChild.Data
			}
			byLineNode, _ := scrape.Find(videoNode, scrape.ByClass("yt-lockup-byline"))
			authorNode, _ := scrape.Find(byLineNode, scrape.ByTag(atom.A))
			video.Author = authorNode.FirstChild.Data
			videos = append(videos, video)
		}
	}
	{ // further pages using ajax

	}

	wh := new(YTWatchHistory)
	wh.Videos = videos
	return wh, nil
}
