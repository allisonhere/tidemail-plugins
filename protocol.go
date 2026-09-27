package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// This file mirrors TideMail's plugin protocol v1 (internal/plugin in the
// TideMail repo). TideMail's package is internal, so the few types needed here
// are copied; contract_test.go checks the output against TideMail's rules.

const (
	apiVersion   = 1
	typeRequest  = "request"
	typeResponse = "response"

	methodPing            = "ping"
	methodMessageMetadata = "message.metadata"
	methodMessageReceived = "message.received" // automatic; same payload and answer
	methodTest            = "plugin.test"

	// maxRequestBytes bounds how much stdin is read. TideMail's requests are a
	// few kilobytes.
	maxRequestBytes = 1 << 20
)

// Error codes returned in ok=false responses.
const (
	codeBadRequest        = "bad_request"
	codeUnsupportedAPI    = "unsupported_api"
	codeUnsupportedMethod = "unsupported_method"
	codeBadMetadata       = "bad_metadata"
)

type request struct {
	API       int    `json:"api"`
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Method    string `json:"method"`
	// Settings are this plugin's resolved settings from TideMail. Secrets are
	// not in here; TideMail passes them in the environment.
	Settings map[string]any  `json:"settings,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
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

// MessageMetadata is TideMail's message.metadata payload. Every field is
// optional; TideMail never sends the body.
type MessageMetadata struct {
	ID            int64    `json:"id"`
	MessageID     string   `json:"message_id,omitempty"`
	From          string   `json:"from,omitempty"`
	To            string   `json:"to,omitempty"`
	CC            string   `json:"cc,omitempty"`
	ReplyTo       string   `json:"reply_to,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	Date          string   `json:"date,omitempty"`
	Read          bool     `json:"read"`
	Starred       bool     `json:"starred"`
	HasAttachment bool     `json:"has_attachment"`
	Flags         []string `json:"flags,omitempty"`
	AccountName   string   `json:"account_name,omitempty"`
	MailboxName   string   `json:"mailbox_name,omitempty"`
}

type pingResult struct {
	Message string `json:"message"`
}

type metadataResult struct {
	Annotations []Annotation `json:"annotations"`
}

// readRequest reads the single request TideMail writes to stdin. An error here
// means there is no request ID to answer, so the caller exits non-zero instead
// of replying.
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
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&req); err != nil {
		return request{}, fmt.Errorf("invalid request JSON: %w", err)
	}
	if req.RequestID == "" {
		return request{}, errors.New("request has no request_id")
	}
	return req, nil
}

// handle answers one parsed request. It always returns a response carrying the
// request's ID. A TypeSafe problem is never an error response: classification
// falls back to the local rules.
func (s smart) handle(ctx context.Context, req request) response {
	switch {
	case req.API != apiVersion:
		return errorResponse(req, codeUnsupportedAPI, fmt.Sprintf("api %d is not supported (want %d)", req.API, apiVersion))
	case req.Type != typeRequest:
		return errorResponse(req, codeBadRequest, fmt.Sprintf("type %q is not a request", req.Type))
	}
	switch req.Method {
	case methodPing:
		return okResponse(req, pingResult{Message: "pong"})
	case methodMessageMetadata, methodMessageReceived:
		meta, err := decodeMetadata(req.Data)
		if err != nil {
			return errorResponse(req, codeBadMetadata, err.Error())
		}
		opts := parseOptions(req.Settings, s.apiKey)
		return okResponse(req, metadataResult{Annotations: s.classify(ctx, meta, opts)})
	case methodTest:
		return okResponse(req, s.testConnection(ctx, parseOptions(req.Settings, s.apiKey)))
	default:
		return errorResponse(req, codeUnsupportedMethod, fmt.Sprintf("method %q is not supported", req.Method))
	}
}

func decodeMetadata(data json.RawMessage) (MessageMetadata, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return MessageMetadata{}, errors.New("message.metadata data must be a JSON object")
	}
	var meta MessageMetadata
	if err := json.Unmarshal(trimmed, &meta); err != nil {
		return MessageMetadata{}, fmt.Errorf("malformed message metadata: %w", err)
	}
	return meta, nil
}

func okResponse(req request, data any) response {
	raw, err := json.Marshal(data)
	if err != nil {
		return errorResponse(req, codeBadRequest, "encode result: "+err.Error())
	}
	return response{API: apiVersion, Type: typeResponse, RequestID: req.RequestID, OK: true, Data: raw}
}

func errorResponse(req request, code, message string) response {
	return response{
		API: apiVersion, Type: typeResponse, RequestID: req.RequestID,
		Error: &protocolError{Code: code, Message: message},
	}
}
