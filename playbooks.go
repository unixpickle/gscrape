package gscrape

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

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
	Title       string   `json:"title"`
	Authors     []string `json:"authors"`
	Publisher   string   `json:"publisher"`
	Description string   `json:"description"`
	PageCount   int      `json:"pageCount"`
	ImageLinks  struct {
		Thumbnail      string `json:"thumbnail"`
		SmallThumbnail string `json:"smallThumbnail"`
	} `json:"imageLinks"`

	// UpdateTimestamp is the last time this book was "updated", which pretty much
	// means opened.
	// This is a UNIX timestamp measured in seconds.
	UpdateTimestamp int64 `json:"updateTime"`

	ID       string `json:"id"`
	Uploaded bool   `json:"uploaded"`
}

type PlayBooks struct {
	s    *Session
	info playBooksAuthInfo
}

// AuthPlayBooks is a wrapper for Authenticate() that uses Google Play Books.
func (s *Session) AuthPlayBooks(email, password string) (*PlayBooks, error) {
	if err := s.Auth("https://play.google.com/books",
		"https://accounts.google.com/ServiceLoginAuth", email, password); err != nil {
		return nil, err
	}
	info, err := s.getPlayBooksAuthInfo()
	if err != nil {
		return nil, err
	}
	return &PlayBooks{s, *info}, nil
}

type userInfo struct {
	Updated  string `json:"updated"`
	Uploaded bool   `json:"isUploaded"`
}

type volumeObject struct {
	ID         string   `json:"id"`
	VolumeInfo BookInfo `json:"volumeInfo"`
	UserInfo   userInfo `json:"userInfo"`
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
			if err != nil {
				errChan <- err
				return
			}
			var fullResponse booksResponse
			if err := json.Unmarshal(contents, &fullResponse); err != nil {
				errChan <- err
				return
			}
			for _, volume := range fullResponse.Items {
				info := volume.VolumeInfo
				if volume.UserInfo.Updated != "" {
					updateDate, _ := time.Parse(time.RFC3339, volume.UserInfo.Updated)
					info.UpdateTimestamp = updateDate.Unix()
				}
				info.ID = volume.ID
				info.Uploaded = volume.UserInfo.Uploaded
				bookChan <- info
			}
			i += len(fullResponse.Items)
			if i >= fullResponse.TotalItems {
				return
			}
		}
	}()

	return bookChan, errChan
}

// Upload adds an E-book to your Play Books library.
// You must specify the size of the book manually, since
// it must be sent to the server before the actual data.
func (p *PlayBooks) Upload(data io.Reader, size int64, filename, title string) error {
	encoded, _ := json.Marshal(map[string]interface{}{
		"protocolVersion": "0.8",
		"createSessionRequest": map[string]interface{}{
			"fields": []interface{}{
				map[string]interface{}{
					"external": map[string]interface{}{
						"name":     "file",
						"filename": filename,
						"put":      map[string]interface{}{},
						"size":     size,
					},
				},
				map[string]interface{}{
					"inlined": map[string]interface{}{
						"name":        "title",
						"contentType": "text/plain",
						"content":     title,
					},
				},
				map[string]interface{}{
					"inlined": map[string]interface{}{
						"name":        "addtime",
						"contentType": "text/plain",
						"content":     strconv.FormatInt(time.Now().UnixNano()/1000000, 10),
					},
				},
			},
		},
	})
	postBody := bytes.NewBuffer(encoded)
	resp, err := p.s.Post("https://docs.google.com/upload/books/library/upload?authuser=0",
		"application/json", postBody)
	if err != nil {
		return err
	}
	contents, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	var startUploadResponse struct {
		SessionStatus struct {
			Transfers []struct {
				PutInfo struct {
					URL string `json:"url"`
				} `json:"putInfo"`
			} `json:"externalFieldTransfers"`
		} `json:"sessionStatus"`
	}
	if err := json.Unmarshal(contents, &startUploadResponse); err != nil {
		return err
	} else if len(startUploadResponse.SessionStatus.Transfers) != 1 {
		return errors.New("unexpected number of transfers")
	}

	uploadURL := startUploadResponse.SessionStatus.Transfers[0].PutInfo.URL
	req, err := http.NewRequest("POST", uploadURL, data)
	if err != nil {
		return err
	}
	req.Header.Set("X-GUploader-No-308", "yes")
	req.Header.Set("X-HTTP-Method-Override", "put")
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err = p.s.Do(req)
	if err != nil {
		return err
	}
	contents, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	var uploadResponse struct {
		SessionStatus struct {
			State      string `json:"state"`
			Additional struct {
				Info struct {
					Info struct {
						Info struct {
							ContentID string `json:"contentId"`
						} `json:"customerSpecificInfo"`
					} `json:"completionInfo"`
				} `json:"uploader_service.GoogleRupioAdditionalInfo"`
			} `json:"additionalInfo"`
		} `json:"sessionStatus"`
	}
	if err := json.Unmarshal(contents, &uploadResponse); err != nil {
		return err
	} else if uploadResponse.SessionStatus.State != "FINALIZED" {
		return errors.New("upload is not finalized")
	}

	contentID := uploadResponse.SessionStatus.Additional.Info.Info.Info.ContentID
	addBookArgs := url.Values{}
	addBookArgs.Set("upload_client_token", contentID)
	addBookArgs.Set("key", p.info.requestKey)
	addBookArgs.Set("source", "ge-books-fe")
	addBookURL := "https://clients6.google.com/books/v1/cloudloading/addBook?" +
		addBookArgs.Encode()
	req, _ = http.NewRequest("POST", addBookURL, nil)
	req.Header.Add("OriginToken", p.info.originToken)
	req.Header.Add("X-Origin", "https://play.google.com")
	resp, err = p.s.Do(req)
	if err != nil {
		return err
	}
	contents, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	var addBookResponse map[string]interface{}
	if err := json.Unmarshal(contents, &addBookResponse); err != nil {
		return err
	}
	if _, ok := addBookResponse["error"]; ok {
		return errors.New("addBook API failed")
	}
	return nil
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
	req.Header.Set("User-Agent", spoofedUserAgent)
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
