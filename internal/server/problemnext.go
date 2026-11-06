package server

import (
	"bytes"
	"encoding/json"
	"mime"
	"net/http"
)

// withProblemNext wraps h so that every problem document it answers carries a
// non-empty `next`. The writers that build a document themselves (writeProblem)
// already do; this covers the documents the generated typed responses write
// from an operation's own value, such as the unknown documentation asset, and
// any added later, so no path can answer a problem without a next action.
func withProblemNext(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pw := &problemWriter{ResponseWriter: w}
		h.ServeHTTP(pw, r)
		pw.finish()
	})
}

// problemWriter holds back the body of a problem+json response until finish can
// complete it; any other response passes straight through.
type problemWriter struct {
	http.ResponseWriter
	held   bool
	status int
	body   bytes.Buffer
}

func (p *problemWriter) WriteHeader(status int) {
	if p.held || p.status != 0 {
		return
	}
	p.status = status
	mt, _, _ := mime.ParseMediaType(p.Header().Get("Content-Type"))
	if mt == "application/problem+json" {
		p.held = true
		return
	}
	p.ResponseWriter.WriteHeader(status)
}

func (p *problemWriter) Write(b []byte) (int, error) {
	if p.status == 0 {
		p.WriteHeader(http.StatusOK)
	}
	if p.held {
		return p.body.Write(b)
	}
	return p.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (p *problemWriter) Unwrap() http.ResponseWriter { return p.ResponseWriter }

func (p *problemWriter) finish() {
	if !p.held {
		return
	}
	var doc map[string]any
	if json.Unmarshal(p.body.Bytes(), &doc) != nil || doc == nil {
		doc = map[string]any{"type": problemType, "title": http.StatusText(p.status), "status": p.status}
	}
	if next, _ := doc["next"].(string); next == "" {
		doc["next"] = defaultNext(p.status)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return
	}
	p.Header().Del("Content-Length")
	p.ResponseWriter.WriteHeader(p.status)
	_, _ = p.ResponseWriter.Write(append(out, '\n'))
}
