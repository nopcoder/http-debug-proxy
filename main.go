package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Helper to read, decompress (if gzip), and restore a ReadCloser body
func readAndMaybeDecompressBody(body io.ReadCloser, encoding string) (rawBody, decodedBody []byte, restore func() io.ReadCloser, err error) {
	rawBody, err = io.ReadAll(body)
	body.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	var decoded []byte
	if encoding == "gzip" {
		gz, err := gzip.NewReader(bytes.NewReader(rawBody))
		if err != nil {
			decoded = rawBody
		} else {
			decoded, err = io.ReadAll(gz)
			gz.Close()
			if err != nil {
				decoded = rawBody
			}
		}
	} else {
		decoded = rawBody
	}
	restore = func() io.ReadCloser {
		return io.NopCloser(bytes.NewReader(rawBody))
	}
	return rawBody, decoded, restore, nil
}

// Dump and log HTTP response headers and body
func dumpHTTPResponse(resp *http.Response) {
	headerDump, err := httputil.DumpResponse(resp, false)
	if err != nil {
		log.Printf("Error dumping response headers: %v", err)
	} else {
		log.Printf("----- RESPONSE HEADERS-----\n%s", headerDump)
	}
	_, decodedBody, restore, err := readAndMaybeDecompressBody(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return
	}
	if decodedBody != nil {
		log.Printf("----- RESPONSE BODY -----\n%s", decodedBody)
	}
	resp.Body = restore()
}

// Dump and log HTTP request headers and body
func dumpHTTPRequest(req *http.Request) {
	headerDump, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		log.Printf("Error dumping request headers: %v", err)
	} else {
		log.Printf("----- REQUEST HEADERS-----\n%s", headerDump)
	}
	if req.Body == nil {
		return
	}
	// Only decompress if Content-Encoding is set
	_, decodedBody, restore, err := readAndMaybeDecompressBody(req.Body, req.Header.Get("Content-Encoding"))
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		return
	}
	if decodedBody != nil {
		log.Printf("----- REQUEST BODY -----\n%s", decodedBody)
	}
	req.Body = restore()
}

// loggingTransport wraps an http.RoundTripper to dump requests
type loggingTransport struct {
	rt http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	dumpHTTPRequest(req)
	return t.rt.RoundTrip(req)
}

func main() {
	listenAddr := flag.String("l", ":9191", "Listen address")
	targetService := flag.String("t", "http://localhost:8181", "Target service")
	flag.Parse()

	target, err := url.Parse(*targetService)
	if err != nil {
		log.Fatalf("Error parsing target service: %v", err)
	}

	// Create the reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &loggingTransport{rt: http.DefaultTransport}
	proxy.ModifyResponse = func(resp *http.Response) error {
		dumpHTTPResponse(resp)
		return nil
	}

	log.Printf("Starting proxy server on %s -> forwarding to %s\n", *listenAddr, target)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})

	if err := http.ListenAndServe(*listenAddr, nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server failed: %v", err)
	}
}
