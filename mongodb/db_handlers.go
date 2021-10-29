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

package mongodb

import (
	"encoding/json"
	"github.com/kamva/mgm/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/martian/v3/log"
)

type exportHandler struct {
	logger *Logger
}

type resetHandler struct {
	logger *Logger
}

// NewMappingHandler returns an http.Handler for requesting HAR logs.
func NewMappingHandler(l *Logger) http.Handler {
	return &exportHandler{
		logger: l,
	}
}

// RetrieveDataHandler returns an http.Handler for clearing in-memory log entries.
func RetrieveDataHandler(l *Logger) http.Handler {
	return &resetHandler{
		logger: l,
	}
}

type mapping struct {
	mgm.DefaultModel `bson:",inline"`
	IPAddress string `json:"ip_address" bson:"ip_address"`
	Package string `json:"package" bson:"package"`
	Platform string `json:"platform" bson:"platform"`
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

type responseBody struct {
	Success bool `json:"success"`
	Message string `json:"message"`
}

// ServeHTTP writes the log in HAR format to the response body.
func (h *exportHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	var m = mapping{}
	if req.Method != "POST" && req.Method != "DELETE" {
		rw.Header().Add("Allow", "POST,DELETE")
		rw.WriteHeader(http.StatusMethodNotAllowed)
		log.Errorf("db.ServeHTTP: method not allowed: %s", req.Method)
		json.NewEncoder(rw).Encode(responseBody{Success: false, Message: "Method not allowed"})
		return
	}

	err := json.NewDecoder(req.Body).Decode(&m)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		json.NewEncoder(rw).Encode(responseBody{Success: false, Message: "Payload could not be parsed"})
		return
	}

	if m.IPAddress == "" || m.Package == "" || m.Platform == "" {
		rw.WriteHeader(http.StatusBadRequest)
		log.Errorf("db.ServeHTTP: IP address, package and platform are required for a mapping", req.Method)
		json.NewEncoder(rw).Encode(responseBody{Success: false, Message: "IP address, package and platform are required for a mapping"})
		return
	}

	if req.Method == "DELETE" {
		//mgm.Coll(&m).First(&m)
		if err := mgm.Coll(&m).Delete(&m); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			json.NewEncoder(rw).Encode(responseBody{Success: false, Message: "Error deleting mapping"})
			return
		}
	}

	log.Infof("mappingHandler.ServeHTTP: adding mapping for IP address %s, package %s and platform %s", m.IPAddress, m.Package, m.Platform)
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	err = mgm.Coll(&m).Create(&m)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		log.Errorf("db.ServeHTTP: error %s", err.Error())
		json.NewEncoder(rw).Encode(responseBody{Success: false, Message: "error writing to database"})
		return
	}

	json.NewEncoder(rw).Encode(m)
}

// ServeHTTP resets the log, which clears its entries.
func (h *resetHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		rw.Header().Add("Allow", "GET")
		rw.WriteHeader(http.StatusMethodNotAllowed)
		log.Errorf("har: method not allowed: %s", req.Method)
		return
	}
	id := req.URL.Query().Get("id")
	if id == "" {
		log.Errorf("har: invalid value for id param")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	var requests []request
	mappingId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		log.Errorf("har: invalid value for id param")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	mgm.Coll(&request{}).SimpleFind(&requests, bson.M{"mapping_id":mappingId})
	json.NewEncoder(rw).Encode(requests)
}

func parseBoolQueryParam(params url.Values, name string) (bool, error) {
	if params[name] == nil {
		return false, nil
	}
	v, err := strconv.ParseBool(params.Get("return"))
	if err != nil {
		return false, err
	}
	return v, nil
}
