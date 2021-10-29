// Copyright 2015 Google Inc. All rights reserved.
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

// Package har collects HTTP requests and responses and stores them in HAR format.
//
// For more information on HAR, see:
// https://w3c.github.io/web-performance/specs/HAR/Overview.html
package rabbitmq

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/google/martian/v3/messageview"
	"github.com/google/martian/v3/proxyutil"
	"github.com/kamva/mgm/v3"
	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/martian/v3"
	"github.com/rs/zerolog/log"
)

// Logger maintains request and response log entries.
type Logger struct {
	bodyLogging     func(*http.Response) bool
	postDataLogging func(*http.Request) bool

	creator *Creator

	mu      sync.Mutex
	entries map[string]*Entry
	tail    *Entry
	channel *amqp.Channel
	queue amqp.Queue
}

// Creator is the program responsible for generating the log. Martian, in this case.
type Creator struct {
	// Name of the log creator application.
	Name string `json:"name"`
	// Version of the log creator application.
	Version string `json:"version"`
}

type request struct {
	mgm.DefaultModel `bson:",inline"`
	MappingId primitive.ObjectID `json:"mapping_id" bson:"mapping_id"`
	ContextId string `json:"context_id" bson:"context_id"`
	Url string `json:"url" bson:"url"`
	Host string `json:"host" bson:"host"`
	Method string `json:"method" bson:"method"`
	ContentLength int64 `json:"content_length" bson:"content_length"`
	UserAgent string `json:"user_agent" bson:"user_agent"`
	OriginIP string `json:"origin_ip" bson:"origin_ip"`
	Package string `json:"package" bson:"package"`
	Platform string `json:"platform" bson:"platform"`
	Details Entry `json:"details" bson:"details"`
	Class string `json:"class" bson:"class"`
}

// Entry is a individual log entry for a request or response.
type Entry struct {
	// ID is the unique ID for the entry.
	ID string `json:"context_id"`
	// StartedDateTime is the date and time stamp of the request start (ISO 8601).
	StartedDateTime time.Time `json:"startedDateTime"`
	// Time is the total elapsed time of the request in milliseconds.
	Time int64 `json:"time"`
	// Request contains the detailed information about the request.
	Request *Request `json:"request"`
	Host string `json:"host"`
	// Response contains the detailed information about the response.
	Response *Response `json:"response,omitempty"`
	// Cache contains information about a request coming from browser cache.
	Cache *Cache `json:"cache"`
	// Timings describes various phases within request-response round trip. All
	// times are specified in milliseconds.
	Timings *Timings `json:"timings"`
	next    *Entry
}

// Request holds data about an individual HTTP request.
type Request struct {
	// Method is the request method (GET, POST, ...).
	Method string `json:"method"`
	// URL is the absolute URL of the request (fragments are not included).
	URL string `json:"url"`
	// HTTPVersion is the Request HTTP version (HTTP/1.1).
	HTTPVersion string `json:"httpVersion"`
	// Cookies is a list of cookies.
	Cookies []Cookie `json:"cookies"`
	// Headers is a list of headers.
	Headers []Header `json:"headers"`
	// QueryString is a list of query parameters.
	QueryString []QueryString `json:"queryString"`
	// PostData is the posted data information.
	PostData *PostData `json:"postData,omitempty"`
	// HeaderSize is the Total number of bytes from the start of the HTTP request
	// message until (and including) the double CLRF before the body. Set to -1
	// if the info is not available.
	HeadersSize int64 `json:"headersSize"`
	// BodySize is the size of the request body (POST data payload) in bytes. Set
	// to -1 if the info is not available.
	BodySize int64 `json:"bodySize"`
}

// Response holds data about an individual HTTP response.
type Response struct {
	// Status is the response status code.
	Status int `json:"status"`
	// StatusText is the response status description.
	StatusText string `json:"statusText"`
	// HTTPVersion is the Response HTTP version (HTTP/1.1).
	HTTPVersion string `json:"httpVersion"`
	// Cookies is a list of cookies.
	Cookies []Cookie `json:"cookies"`
	// Headers is a list of headers.
	Headers []Header `json:"headers"`
	// Content contains the details of the response body.
	Content *Content `json:"content"`
	// RedirectURL is the target URL from the Location response header.
	RedirectURL string `json:"redirectURL"`
	// HeadersSize is the total number of bytes from the start of the HTTP
	// request message until (and including) the double CLRF before the body.
	// Set to -1 if the info is not available.
	HeadersSize int64 `json:"headersSize"`
	// BodySize is the size of the request body (POST data payload) in bytes. Set
	// to -1 if the info is not available.
	BodySize int64 `json:"bodySize"`
}

// Cache contains information about a request coming from browser cache.
type Cache struct {
	// Has no fields as they are not supported, but HAR requires the "cache"
	// object to exist.
}

// Timings describes various phases within request-response round trip. All
// times are specified in milliseconds
type Timings struct {
	// Send is the time required to send HTTP request to the server.
	Send int64 `json:"send"`
	// Wait is the time spent waiting for a response from the server.
	Wait int64 `json:"wait"`
	// Receive is the time required to read entire response from server or cache.
	Receive int64 `json:"receive"`
}

// Cookie is the data about a cookie on a request or response.
type Cookie struct {
	// Name is the cookie name.
	Name string `json:"name"`
	// Value is the cookie value.
	Value string `json:"value"`
	// Path is the path pertaining to the cookie.
	Path string `json:"path,omitempty"`
	// Domain is the host of the cookie.
	Domain string `json:"domain,omitempty"`
	// Expires contains cookie expiration time.
	Expires time.Time `json:"-"`
	// Expires8601 contains cookie expiration time in ISO 8601 format.
	Expires8601 string `json:"expires,omitempty"`
	// HTTPOnly is set to true if the cookie is HTTP only, false otherwise.
	HTTPOnly bool `json:"httpOnly,omitempty"`
	// Secure is set to true if the cookie was transmitted over SSL, false
	// otherwise.
	Secure bool `json:"secure,omitempty"`
}

// Header is an HTTP request or response header.
type Header struct {
	// Name is the header name.
	Name string `json:"name"`
	// Value is the header value.
	Value string `json:"value"`
}

// QueryString is a query string parameter on a request.
type QueryString struct {
	// Name is the query parameter name.
	Name string `json:"name"`
	// Value is the query parameter value.
	Value string `json:"value"`
}

// PostData describes posted data on a request.
type PostData struct {
	// MimeType is the MIME type of the posted data.
	MimeType string `json:"mimeType"`
	// Params is a list of posted parameters (in case of URL encoded parameters).
	Params []Param `json:"params"`
	// Text contains the posted data. Although its type is string, it may contain
	// binary data.
	Text string `json:"text"`
}

// pdBinary is the JSON representation of binary PostData.
type pdBinary struct {
	MimeType string `json:"mimeType"`
	// Params is a list of posted parameters (in case of URL encoded parameters).
	Params   []Param `json:"params"`
	Text     []byte  `json:"text"`
	Encoding string  `json:"encoding"`
}

// MarshalJSON returns a JSON representation of binary PostData.
func (p *PostData) MarshalJSON() ([]byte, error) {
	if utf8.ValidString(p.Text) {
		type noMethod PostData // avoid infinite recursion
		return json.Marshal((*noMethod)(p))
	}
	return json.Marshal(pdBinary{
		MimeType: p.MimeType,
		Params:   p.Params,
		Text:     []byte(p.Text),
		Encoding: "base64",
	})
}

// UnmarshalJSON populates PostData based on the []byte representation of
// the binary PostData.
func (p *PostData) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) { // conform to json.Unmarshaler spec
		return nil
	}
	var enc struct {
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(data, &enc); err != nil {
		return err
	}
	if enc.Encoding != "base64" {
		type noMethod PostData // avoid infinite recursion
		return json.Unmarshal(data, (*noMethod)(p))
	}
	var pb pdBinary
	if err := json.Unmarshal(data, &pb); err != nil {
		return err
	}
	p.MimeType = pb.MimeType
	p.Params = pb.Params
	p.Text = string(pb.Text)
	return nil
}

// Param describes an individual posted parameter.
type Param struct {
	// Name of the posted parameter.
	Name string `json:"name"`
	// Value of the posted parameter.
	Value string `json:"value,omitempty"`
	// Filename of a posted file.
	Filename string `json:"fileName,omitempty"`
	// ContentType is the content type of a posted file.
	ContentType string `json:"contentType,omitempty"`
}

// Content describes details about response content.
type Content struct {
	// Size is the length of the returned content in bytes. Should be equal to
	// response.bodySize if there is no compression and bigger when the content
	// has been compressed.
	Size int64 `json:"size"`
	// MimeType is the MIME type of the response text (value of the Content-Type
	// response header).
	MimeType string `json:"mimeType"`
	// Text contains the response body sent from the server or loaded from the
	// browser cache. This field is populated with fully decoded version of the
	// respose body.
	Text []byte `json:"text,omitempty"`
	// The desired encoding to use for the text field when encoding to JSON.
	Encoding string `json:"encoding,omitempty"`
}

// For marshaling Content to and from json. This works around the json library's
// default conversion of []byte to base64 encoded string.
type contentJSON struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`

	// Text contains the response body sent from the server or loaded from the
	// browser cache. This field is populated with textual content only. The text
	// field is either HTTP decoded text or a encoded (e.g. "base64")
	// representation of the response body. Leave out this field if the
	// information is not available.
	Text string `json:"text,omitempty"`

	// Encoding used for response text field e.g "base64". Leave out this field
	// if the text field is HTTP decoded (decompressed & unchunked), than
	// trans-coded from its original character set into UTF-8.
	Encoding string `json:"encoding,omitempty"`
}

// MarshalJSON marshals the byte slice into json after encoding based on c.Encoding.
func (c Content) MarshalJSON() ([]byte, error) {
	var txt string
	switch c.Encoding {
	case "base64":
		txt = base64.StdEncoding.EncodeToString(c.Text)
	case "":
		txt = string(c.Text)
	default:
		return nil, fmt.Errorf("unsupported encoding for Content.Text: %s", c.Encoding)
	}

	cj := contentJSON{
		Size:     c.Size,
		MimeType: c.MimeType,
		Text:     txt,
		Encoding: c.Encoding,
	}
	return json.Marshal(cj)
}

// UnmarshalJSON unmarshals the bytes slice into Content.
func (c *Content) UnmarshalJSON(data []byte) error {
	var cj contentJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return err
	}

	var txt []byte
	var err error
	switch cj.Encoding {
	case "base64":
		txt, err = base64.StdEncoding.DecodeString(cj.Text)
		if err != nil {
			return fmt.Errorf("failed to decode base64-encoded Content.Text: %v", err)
		}
	case "":
		txt = []byte(cj.Text)
	default:
		return fmt.Errorf("unsupported encoding for Content.Text: %s", cj.Encoding)
	}

	c.Size = cj.Size
	c.MimeType = cj.MimeType
	c.Text = txt
	c.Encoding = cj.Encoding
	return nil
}

// Option is a configurable setting for the custom_logger.
type Option func(l *Logger)

// PostDataLogging returns an option that configures request post data logging.
func PostDataLogging(enabled bool) Option {
	return func(l *Logger) {
		l.postDataLogging = func(*http.Request) bool {
			return enabled
		}
	}
}

// BodyLogging returns an option that configures response body logging.
func BodyLogging(enabled bool) Option {
	return func(l *Logger) {
		l.bodyLogging = func(*http.Response) bool {
			return enabled
		}
	}
}

// NewLogger returns a HAR custom_logger. The returned
// custom_logger logs all request post data and response bodies by default.
func NewLogger(channel *amqp.Channel, queue amqp.Queue) *Logger {
	l := &Logger{
		creator: &Creator{
			Name:    "martian proxy",
			Version: "2.0.0",
		},
		channel: channel,
		queue: queue,
		entries: make(map[string]*Entry),
	}
	l.SetOption(BodyLogging(true))
	l.SetOption(PostDataLogging(true))
	return l
}

// SetOption sets configurable options on the custom_logger.
func (l *Logger) SetOption(opts ...Option) {
	for _, opt := range opts {
		opt(l)
	}
}

// ModifyRequest logs requests.
func (l *Logger) ModifyRequest(req *http.Request) error {
	ctx := martian.NewContext(req)
	if ctx.SkippingLogging() {
		return nil
	}

	id := ctx.ID()

	return l.RecordRequest(id, req)
}

// RecordRequest logs the HTTP request with the given ID. The ID should be unique
// per request/response pair.
func (l *Logger) RecordRequest(id string, req *http.Request) error {

	hreq, err := NewRequest(req, l.postDataLogging(req))
	if err != nil {
		return err
	}

	entry := &Entry{
		ID:              id,
		StartedDateTime: time.Now().UTC(),
		Request:         hreq,
		Host:			 req.Host,
		Cache:           &Cache{},
		Timings:         &Timings{},
	}

	body, err := json.Marshal(entry)
	if err != nil {
		log.Error().
			Str("context-id", id).
			Str("origin-ip", req.Header.Get("X-Forwarded-For")).
			Str("host", req.Host).
			Msg("Error serializing request")
	}

	if err = l.channel.Publish("",     // exchange
		l.queue.Name, // routing key
		false,  // mandatory
		false,  // immediate
		amqp.Publishing {
			ContentType: "application/json",
			Body:        body,
		}); err != nil {
		log.Error().
			Str("context-id", id).
			Str("origin-ip", req.Header.Get("X-Forwarded-For")).
			Str("host", req.Host).
			Msg("Error publishing request.")
		return nil
	}

	log.Info().
			Str("context-id", id).
			Str("origin-ip", req.Header.Get("X-Forwarded-For")).
			Str("host", req.Host).
			Msg("Enqueued request.")
	return nil
}

// ModifyResponse logs responses.
func (l *Logger) ModifyResponse(res *http.Response) error {
	ctx := martian.NewContext(res.Request)
	if ctx.SkippingLogging() {
		return nil
	}
	id := ctx.ID()

	return l.RecordResponse(id, res)
}

// RecordResponse logs an HTTP response, associating it with the previously-logged
// HTTP request with the same ID.
func (l *Logger) RecordResponse(id string, res *http.Response) error {
	return nil
}

// NewRequest constructs and returns a Request from req. If withBody is true,
// req.Body is read to EOF and replaced with a copy in a bytes.Buffer. An error
// is returned (and req.Body may be in an intermediate state) if an error is
// returned from req.Body.Read.
func NewRequest(req *http.Request, withBody bool) (*Request, error) {
	r := &Request{
		Method:      req.Method,
		URL:         req.URL.String(),
		HTTPVersion: req.Proto,
		HeadersSize: -1,
		BodySize:    req.ContentLength,
		QueryString: []QueryString{},
		Headers:     headers(proxyutil.RequestHeader(req).Map()),
		Cookies:     cookies(req.Cookies()),
	}

	for n, vs := range req.URL.Query() {
		for _, v := range vs {
			r.QueryString = append(r.QueryString, QueryString{
				Name:  n,
				Value: v,
			})
		}
	}

	pd, err := postData(req, withBody)
	if err != nil {
		return nil, err
	}
	r.PostData = pd

	return r, nil
}

func cookies(cs []*http.Cookie) []Cookie {
	hcs := make([]Cookie, 0, len(cs))

	for _, c := range cs {
		var expires string
		if !c.Expires.IsZero() {
			expires = c.Expires.Format(time.RFC3339)
		}

		hcs = append(hcs, Cookie{
			Name:        c.Name,
			Value:       c.Value,
			Path:        c.Path,
			Domain:      c.Domain,
			HTTPOnly:    c.HttpOnly,
			Secure:      c.Secure,
			Expires:     c.Expires,
			Expires8601: expires,
		})
	}

	return hcs
}

func headers(hs http.Header) []Header {
	hhs := make([]Header, 0, len(hs))

	for n, vs := range hs {
		for _, v := range vs {
			hhs = append(hhs, Header{
				Name:  n,
				Value: v,
			})
		}
	}

	return hhs
}

func postData(req *http.Request, logBody bool) (*PostData, error) {
	// If the request has no body (no Content-Length and Transfer-Encoding isn't
	// chunked), skip the post data.
	if req.ContentLength <= 0 && len(req.TransferEncoding) == 0 {
		return nil, nil
	}

	ct := req.Header.Get("Content-Type")
	mt, ps, err := mime.ParseMediaType(ct)
	if err != nil {
		log.Error().Msgf("har: cannot parse Content-Type header %q: %v", ct, err)
		mt = ct
	}

	pd := &PostData{
		MimeType: mt,
		Params:   []Param{},
	}

	if !logBody {
		return pd, nil
	}

	mv := messageview.New()
	if err := mv.SnapshotRequest(req); err != nil {
		return nil, err
	}

	br, err := mv.BodyReader()
	if err != nil {
		return nil, err
	}

	switch mt {
	case "multipart/form-data":
		mpr := multipart.NewReader(br, ps["boundary"])

		for {
			p, err := mpr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			defer p.Close()

			body, err := ioutil.ReadAll(p)
			if err != nil {
				return nil, err
			}

			pd.Params = append(pd.Params, Param{
				Name:        p.FormName(),
				Filename:    p.FileName(),
				ContentType: p.Header.Get("Content-Type"),
				Value:       string(body),
			})
		}
	case "application/x-www-form-urlencoded":
		body, err := ioutil.ReadAll(br)
		if err != nil {
			return nil, err
		}

		vs, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}

		for n, vs := range vs {
			for _, v := range vs {
				pd.Params = append(pd.Params, Param{
					Name:  n,
					Value: v,
				})
			}
		}
	default:
		body, err := ioutil.ReadAll(br)
		if err != nil {
			return nil, err
		}

		pd.Text = string(body)
	}

	return pd, nil
}