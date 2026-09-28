package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// The parts of TideMail's plugin protocol v1 this plugin uses
// (docs/plugins/protocol.md in TideMail).

const (
	apiVersion      = 1
	maxRequestBytes = 1 << 20

	methodPing            = "ping"
	methodMessageMetadata = "message.metadata"
	methodMessageReceived = "message.received"
)

type request struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type response struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     *protocolError  `json:"error,omitempty"`
}

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// metadata is TideMail's message.metadata payload: headers only, never the
// body. API v1 does not include list headers (List-ID, List-Unsubscribe,
// Precedence); see headers in classify.go.
type metadata struct {
	ID            int64    `json:"id"`
	MessageID     string   `json:"message_id,omitempty"`
	From          string   `json:"from,omitempty"`
	To            string   `json:"to,omitempty"`
	ReplyTo       string   `json:"reply_to,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	Date          string   `json:"date,omitempty"`
	HasAttachment bool     `json:"has_attachment"`
	Flags         []string `json:"flags,omitempty"`
	AccountName   string   `json:"account_name,omitempty"`
	MailboxName   string   `json:"mailbox_name,omitempty"`
}

type annotationsResult struct {
	Annotations  []annotation  `json:"annotations"`
	Presentation *presentation `json:"presentation,omitempty"`
}

func readRequest(r io.Reader) (request, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil {
		return request{}, fmt.Errorf("read request: %w", err)
	}
	if len(raw) > maxRequestBytes {
		return request{}, errors.New("request too large")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return request{}, errors.New("empty request")
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

func handle(req request) response {
	switch {
	case req.API != apiVersion:
		return errorResponse(req, "unsupported_api", fmt.Sprintf("api %d is not supported (want %d)", req.API, apiVersion))
	case req.Type != "request":
		return errorResponse(req, "bad_request", fmt.Sprintf("type %q is not a request", req.Type))
	}
	switch req.Method {
	case methodPing:
		return okResponse(req, map[string]string{"message": "pong"})
	case methodMessageMetadata, methodMessageReceived:
		trimmed := bytes.TrimSpace(req.Data)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return errorResponse(req, "bad_metadata", "message metadata must be a JSON object")
		}
		var meta metadata
		if err := json.Unmarshal(trimmed, &meta); err != nil {
			return errorResponse(req, "bad_metadata", "malformed message metadata")
		}
		// API v1 exposes no list headers, so header signals stay empty.
		v := classify(meta, headers{})
		p := v.presentation()
		return okResponse(req, annotationsResult{Annotations: v.annotations(), Presentation: &p})
	}
	return errorResponse(req, "unsupported_method", fmt.Sprintf("method %q is not supported", req.Method))
}

func okResponse(req request, data any) response {
	raw, err := json.Marshal(data)
	if err != nil {
		return errorResponse(req, "bad_request", "encode result: "+err.Error())
	}
	return response{API: apiVersion, Type: "response", RequestID: req.RequestID, OK: true, Data: raw}
}

func errorResponse(req request, code, message string) response {
	return response{API: apiVersion, Type: "response", RequestID: req.RequestID, Error: &protocolError{Code: code, Message: message}}
}
