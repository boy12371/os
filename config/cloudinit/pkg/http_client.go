// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkg

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/sveil/os/pkg/log"
)

const (
	HTTP2xx = 2
	HTTP4xx = 4
)

type Err error

type ErrTimeout struct {
	Err
}

type ErrNotFound struct {
	Err
}

type ErrInvalid struct {
	Err
}

type ErrServer struct {
	Err
}

type ErrNetwork struct {
	Err
}

type HTTPClient struct {
	// Initial backoff duration. Defaults to 50 milliseconds
	InitialBackoff time.Duration

	// Maximum exp backoff duration. Defaults to 5 seconds
	MaxBackoff time.Duration

	// Maximum number of connection retries. Defaults to 15
	MaxRetries int

	// Headers to add to the request.
	Header http.Header

	client *http.Client
}

type Getter interface {
	Get(string) ([]byte, error)
	GetRetry(string) ([]byte, error)
}

func NewHTTPClient() *HTTPClient {
	return NewHTTPClientHeader(nil)
}

func NewHTTPClientHeader(header http.Header) *HTTPClient {
	hc := &HTTPClient{
		InitialBackoff: 50 * time.Millisecond,
		MaxBackoff:     time.Second * 5,
		MaxRetries:     15,
		Header:         header,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	return hc
}

func ExpBackoff(interval, max time.Duration) time.Duration {
	interval = interval * 2
	if interval > max {
		interval = max
	}
	return interval
}

// GetRetry fetches a given URL with support for exponential backoff and maximum retries
func (h *HTTPClient) GetRetry(rawurl string) ([]byte, error) {
	if rawurl == "" {
		return nil, ErrInvalid{errors.New("URL is empty. Skipping")}
	}

	url, err := neturl.Parse(rawurl)
	if err != nil {
		return nil, ErrInvalid{err}
	}

	// Unfortunately, url.Parse is too generic to throw errors if a URL does not
	// have a valid HTTP scheme. So, we have to do this extra validation
	if !strings.HasPrefix(url.Scheme, "http") {
		return nil, ErrInvalid{fmt.Errorf("URL %s does not have a valid HTTP scheme. Skipping", rawurl)}
	}

	dataURL := url.String()

	duration := h.InitialBackoff
	for retry := 1; retry <= h.MaxRetries; retry++ {
		log.Debugf("Fetching data from %s. Attempt #%d", dataURL, retry)

		data, err := h.Get(dataURL)
		switch err.(type) {
		case ErrNetwork:
			log.Debugf(err.Error())
		case ErrServer:
			log.Debugf(err.Error())
		case ErrNotFound:
			return data, err
		default:
			return data, err
		}

		duration = ExpBackoff(duration, h.MaxBackoff)
		log.Debugf("Sleeping for %v...", duration)
		time.Sleep(duration)
	}

	return nil, ErrTimeout{fmt.Errorf("Unable to fetch data. Maximum retries reached: %d", h.MaxRetries)}
}

func (h *HTTPClient) Get(dataURL string) ([]byte, error) {
	request, err := http.NewRequest("GET", dataURL, nil)
	if err != nil {
		return nil, err
	}

	request.Header = h.Header
	if resp, err := h.client.Do(request); err == nil {
		defer resp.Body.Close()
		switch resp.StatusCode / 100 {
		case HTTP2xx:
			return ioutil.ReadAll(resp.Body)
		case HTTP4xx:
			return nil, ErrNotFound{fmt.Errorf("Not found. HTTP status code: %d", resp.StatusCode)}
		default:
			return nil, ErrServer{fmt.Errorf("Server error. HTTP status code: %d", resp.StatusCode)}
		}
	} else {
		return nil, ErrNetwork{fmt.Errorf("Unable to fetch data: %s", err.Error())}
	}
}
