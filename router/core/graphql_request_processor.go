package core

import (
	"encoding/json"
	"net/http"
	"slices"
	"sync"
)

var batchHeadersToRemove = []string{
	"Content-Type",
	"Content-Length",
	"Transfer-Encoding",
	"Encoding",
	// some of these could be moved to extensions potentially
	"Etag",
	"Cache-Control",
	"Expires",
	"Last-Modified",
	"Vary",
}

type batchResponseWriter struct {
	header http.Header
	code   int
	bytes  []byte
}

func (m *batchResponseWriter) Header() http.Header {
	return m.header
}

func (m *batchResponseWriter) Write(p []byte) (int, error) {
	m.bytes = append(m.bytes, p...)
	return len(p), nil
}

func (m *batchResponseWriter) WriteHeader(code int) {
	m.code = code
}

type ProcessFn func(rw http.ResponseWriter, r *http.Request, body []byte)

const arr_char = '['

func ProcessRequest(
	rw http.ResponseWriter,
	r *http.Request,
	body []byte,
	f ProcessFn) error {

	if body[0] != arr_char {
		f(rw, r, body)
		return nil
	}

	var ops []json.RawMessage
	err := json.Unmarshal(body, &ops)

	if err != nil {
		return err
	}

	wg := &sync.WaitGroup{}

	l := len(ops)

	writers := make([]*batchResponseWriter, l)

	for i, b := range ops {
		writers[i] = &batchResponseWriter{
			header: make(http.Header),
		}
		wg.Add(1)

		go func(writer http.ResponseWriter, b []byte) {
			defer wg.Done()
			f(writer, r.Clone(r.Context()), b)
		}(writers[i], b)
	}

	wg.Wait()

	var bufs = make([]json.RawMessage, l)
	for i, writer := range writers {
		headers := writer.Header()

		for _, k := range batchHeadersToRemove {
			headers.Del(k)
		}

		for k, vs := range headers {
			for _, v := range vs {
				if !slices.Contains(rw.Header().Values(k), v) {
					rw.Header().Add(k, v)
				}
			}
		}

		bufs[i] = json.RawMessage(writer.bytes)
	}

	data, err := json.Marshal(bufs)

	if err != nil {
		return err
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write(data)

	return nil
}
