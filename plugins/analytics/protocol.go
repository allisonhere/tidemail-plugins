package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// This file mirrors the parts of TideMail's plugin protocol v1 this plugin
// uses: the envelopes and report.run (docs/plugins/queries.md in TideMail).

const (
	apiVersion      = 1
	maxRequestBytes = 8 << 20 // report rounds carry query results
)

type request struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Settings  map[string]any  `json:"settings,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type response struct {
	API       int        `json:"api"`
	Type      string     `json:"type"`
	RequestID string     `json:"request_id"`
	OK        bool       `json:"ok"`
	Data      any        `json:"data,omitempty"`
	Error     *errorBody `json:"error,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// reportContext says where and when the report started.
type reportContext struct {
	Now      string `json:"now"`
	Timezone string `json:"timezone"`
	// Views is the structured-view version TideMail draws; 0 on TideMail
	// builds without views, which get the text dashboard instead.
	Views int `json:"views,omitempty"`
}

// reportRequest is report.run's data.
type reportRequest struct {
	Round   int                        `json:"round"`
	Context reportContext              `json:"context"`
	State   json.RawMessage            `json:"state,omitempty"`
	Results map[string]json.RawMessage `json:"results,omitempty"`
}

// query is one read-only query for TideMail to run.
type query map[string]any

// reportStep is a report.run answer: more queries, or the finished report.
type reportStep struct {
	Queries map[string]query `json:"queries,omitempty"`
	State   *state           `json:"state,omitempty"`
	Report  string           `json:"report,omitempty"`
	View    *view            `json:"view,omitempty"`
}

func readRequest(r io.Reader) (request, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil {
		return request{}, fmt.Errorf("read request: %w", err)
	}
	if len(raw) > maxRequestBytes {
		return request{}, errors.New("request too large")
	}
	var req request
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&req); err != nil {
		return request{}, fmt.Errorf("invalid request JSON: %w", err)
	}
	if req.RequestID == "" {
		return request{}, errors.New("request has no request_id")
	}
	return req, nil
}

func ok(req request, data any) response {
	return response{API: apiVersion, Type: "response", RequestID: req.RequestID, OK: true, Data: data}
}

func fail(req request, code, message string) response {
	return response{API: apiVersion, Type: "response", RequestID: req.RequestID, Error: &errorBody{Code: code, Message: message}}
}
