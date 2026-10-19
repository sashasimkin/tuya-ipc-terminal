package cameras

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type discoveryDiagnostics struct {
	enabled bool
	output  io.Writer
}

func newDiscoveryDiagnostics(enabled bool, output io.Writer) discoveryDiagnostics {
	if !enabled {
		output = io.Discard
	}
	return discoveryDiagnostics{enabled: enabled, output: output}
}

func (d discoveryDiagnostics) log(format string, args ...interface{}) {
	fmt.Fprintf(d.output, "discovery: "+format+"\n", args...)
}

func (d discoveryDiagnostics) failure(stage string, err error) {
	kind := "api"
	var transportError *url.Error
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	switch {
	case errors.As(err, &transportError):
		kind = "transport"
	case errors.As(err, &syntaxError), errors.As(err, &typeError):
		kind = "json"
	case strings.HasPrefix(err.Error(), "HTTP error:"):
		kind = "http"
	}
	// API errors can contain response bodies, URLs and credentials.
	d.log("stage=%s failure=%s", stage, kind)
}

var diagnosticCategory = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func (d discoveryDiagnostics) device(source, category string, supported, duplicate bool) {
	decision := "accepted"
	if !supported {
		decision = "unsupported"
	} else if duplicate {
		decision = "duplicate"
	}
	if !diagnosticCategory.MatchString(category) {
		category = "redacted"
	}
	d.log("stage=filter source=%s category=%s decision=%s", source, category, decision)
}

type discoveryTransport struct {
	base        http.RoundTripper
	diagnostics discoveryDiagnostics
}

func (t discoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	stage := "other"
	switch req.URL.Path {
	case "/api/customized/web/app/info":
		stage = "session"
	case "/api/new/common/homeList":
		stage = "homes"
	case "/api/new/common/roomList":
		stage = "rooms"
	case "/api/new/playback/shareList":
		stage = "shared"
	case "/api/jarvis/config":
		stage = "webrtc"
	}
	resp, err := t.base.RoundTrip(req)
	if resp != nil {
		t.diagnostics.log("stage=%s http_status=%d", stage, resp.StatusCode)
	}
	return resp, err
}
