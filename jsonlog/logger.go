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

// Package martianlog provides a Martian modifier that logs the request and response.
package jsonlog

import (
	"encoding/json"
	"github.com/google/martian/v3/parse"
	"github.com/rs/zerolog/log"
	"net/http"
)

// Logger is a modifier that logs requests and responses.
type Logger struct {
	log         func(line string)
	headersOnly bool
	decode      bool
}

type loggerJSON struct {
	Scope       []parse.ModifierType `json:"scope"`
	HeadersOnly bool                 `json:"headersOnly"`
	Decode      bool                 `json:"decode"`
}

func init() {
	parse.Register("log.JsonLogger", loggerFromJSON)
}

type myLoggerType struct {
	Tester string `json:"tester"`
	Mykey string `json:"mykey"`
}

// NewLogger returns a custom_logger that logs requests and responses, optionally
// logging the body. Log function defaults to martian.Infof.
func NewLogger() *Logger {
	return &Logger{
		log: func(line string) {
			log.Info().Msg("asd")
		},
	}
}

// SetHeadersOnly sets whether to log the request/response body in the log.
func (l *Logger) SetHeadersOnly(headersOnly bool) {
	l.headersOnly = headersOnly
}

// SetDecode sets whether to decode the request/response body in the log.
func (l *Logger) SetDecode(decode bool) {
	l.decode = decode
}

// SetLogFunc sets the logging function for the custom_logger.
func (l *Logger) SetLogFunc(logFunc func(line string)) {
	l.log = logFunc
}

// ModifyRequest logs the request, optionally including the body.
//
// The format logged is:
// --------------------------------------------------------------------------------
// Request to http://www.google.com/path?querystring
// --------------------------------------------------------------------------------
// GET /path?querystring HTTP/1.1
// Host: www.google.com
// Connection: close
// Other-Header: values
//
// request content
// --------------------------------------------------------------------------------
func (l *Logger) ModifyRequest(req *http.Request) error {
	log.Info().
		Str("logger", "request").
		Str("url", req.URL.String()).
		Str("method", req.Method).
		Str("host", req.Host).
		Int64("content-length", req.ContentLength).
		Str("user-agent", req.UserAgent()).
		Str("origin-ip", req.Header.Get("X-Forwarded-For")).
		Msg("request received")

	return nil
}

// ModifyResponse logs the response, optionally including the body.
//
// The format logged is:
// --------------------------------------------------------------------------------
// Response from http://www.google.com/path?querystring
// --------------------------------------------------------------------------------
// HTTP/1.1 200 OK
// Date: Tue, 15 Nov 1994 08:12:31 GMT
// Other-Header: values
//
// response content
// --------------------------------------------------------------------------------
func (l *Logger) ModifyResponse(res *http.Response) error {
	log.Info().
		Str("logger", "request").
		Str("url", res.Request.URL.String()).
		Int("status-code", res.StatusCode).
		Str("status", res.Status).
		Int64("content-length", res.ContentLength).
		Msg("response")

	return nil
}

// loggerFromJSON builds a custom_logger from JSON.
//
// Example JSON:
// {
//   "log.Logger": {
//     "scope": ["request", "response"],
//		 "headersOnly": true,
//		 "decode": true
//   }
// }
func loggerFromJSON(b []byte) (*parse.Result, error) {
	msg := &loggerJSON{}
	if err := json.Unmarshal(b, msg); err != nil {
		return nil, err
	}

	l := NewLogger()
	l.SetHeadersOnly(msg.HeadersOnly)
	l.SetDecode(msg.Decode)

	return parse.NewResult(l, msg.Scope)
}
