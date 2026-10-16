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
	Settings  map[string]any  `json:"settings,omitempty"`
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
// body. Auth is absent on older TideMail versions and on messages without
// sender-check headers; the classifier treats that as "unknown".
type metadata struct {
	ID            int64    `json:"id"`
	From          string   `json:"from,omitempty"`
	To            string   `json:"to,omitempty"`
	ReplyTo       string   `json:"reply_to,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	HasAttachment bool     `json:"has_attachment"`
	Flags         []string `json:"flags,omitempty"`
	Auth          *auth    `json:"auth,omitempty"`
}

// auth is what the user's mail server recorded about the sender. It is
// evidence, not proof: the values are only as trustworthy as that server.
type auth struct {
	SPF      string `json:"spf,omitempty"`
	DKIM     string `json:"dkim,omitempty"`
	DMARC    string `json:"dmarc,omitempty"`
	SpamFlag bool   `json:"spam_flag,omitempty"`
}

type annotation struct {
	Key        string   `json:"key"`
	Value      string   `json:"value"`
	Confidence *float64 `json:"confidence,omitempty"`
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
		sens, _ := req.Settings["sensitivity"].(string)
		v := classify(meta, sensitivityFor(sens))
		res := annotationsResult{Annotations: v.annotations()}
		if p := v.presentation(meta); p != nil {
			res.Presentation = p
		}
		return okResponse(req, res)
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
