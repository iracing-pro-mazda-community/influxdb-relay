package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/influxdata/influxdb/models"
	"github.com/vente-privee/influxdb-relay/config"
)

func (h *HTTP) handleStatus(w http.ResponseWriter, r *http.Request, _ time.Time) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		st := make(map[string]map[string]string)

		for _, b := range h.backends {
			st[b.name] = b.poster.getStats()
		}

		j, _ := json.Marshal(st)

		jsonResponse(w, response{http.StatusOK, fmt.Sprintf("\"status\": %s", string(j))})
	} else {
		jsonResponse(w, response{http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)})
		return
	}
}

func (h *HTTP) handlePing(w http.ResponseWriter, r *http.Request, _ time.Time) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		for key, value := range h.pingResponseHeaders {
			w.Header().Add(key, value)
		}
		w.WriteHeader(h.pingResponseCode)
	} else {
		jsonResponse(w, response{http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)})
		return
	}
}

type health struct {
	name     string
	err      error
	duration time.Duration
}

type healthReport struct {
	Status  string            `json:"status"`
	Healthy map[string]string `json:"healthy,omitempty"`
	Problem map[string]string `json:"problem,omitempty"`
}

var (
	errorCreateRequest = errors.New("Unable to prepare request")
)

func (h *HTTP) handleHealth(w http.ResponseWriter, _ *http.Request, _ time.Time) {
	var responses = make(chan health, len(h.backends))
	var wg sync.WaitGroup
	var client = http.Client{}
	var validEndpoints = 0
	wg.Add(len(h.backends))

	for _, b := range h.backends {
		b := b

		if b.admin == "" {
			wg.Done()
			continue
		}

		validEndpoints++

		go func() {
			defer wg.Done()

			var healthCheck = health{name: b.name, err: nil}

			req, err := http.NewRequest("GET", b.admin+"/ping", nil)
			if err != nil {
				healthCheck.err = errorCreateRequest
				responses <- healthCheck
				return
			}

			start := time.Now()
			res, err := client.Do(req)
			if err != nil {
				healthCheck.err = err
			} else {
				if res.StatusCode/100 != 2 {
					healthCheck.err = errors.New("Unexpected error code " + string(res.StatusCode))
				}
				healthCheck.duration = time.Since(start)
			}
			responses <- healthCheck
			return
		}()
	}

	go func() {
		wg.Wait()
		close(responses)
	}()

	nbDown := 0
	report := healthReport{}
	for r := range responses {
		if r.err == nil {
			if report.Healthy == nil {
				report.Healthy = make(map[string]string)
			}
			report.Healthy[r.name] = "OK. Time taken " + r.duration.String()

		} else {
			if report.Problem == nil {
				report.Problem = make(map[string]string)
			}
			report.Problem[r.name] = "KO. " + r.err.Error()
			nbDown++
		}
	}
	switch {
	case nbDown == validEndpoints:
		report.Status = "critical"
	case nbDown >= 1:
		report.Status = "problem"
	case nbDown == 0:
		report.Status = "healthy"
	}
	response := response{code: 200, body: report}
	jsonResponse(w, response)
	return
}

func (h *HTTP) handleAdmin(w http.ResponseWriter, r *http.Request, _ time.Time) {
	// Client to perform the raw queries
	client := http.Client{}

	if r.Method != http.MethodPost {
		// Bad method
		w.Header().Set("Allow", http.MethodPost)
		jsonResponse(w, response{http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)})
		return
	}
	// Responses
	var responses = make(chan *http.Response, len(h.backends))

	// Associated waitgroup
	var wg sync.WaitGroup
	wg.Add(len(h.backends))

	// Iterate over all backends
	for _, b := range h.backends {
		b := b

		if b.admin == "" {
			// Empty query, skip backend
			wg.Done()
			continue
		}

		go func() {
			defer wg.Done()

			// Create new request
			// Update location according to backend
			// Forward body
			req, err := http.NewRequest("POST", b.admin+"/query", r.Body)
			if err != nil {
				log.Printf("Problem posting to relay %q backend %q: could not prepare request: %v", h.Name(), b.name, err)
				responses <- &http.Response{}
				return
			}

			// Forward headers
			req.Header = r.Header

			// Forward the request
			resp, err := client.Do(req)
			if err != nil {
				// Internal error
				log.Printf("Problem posting to relay %q backend %q: %v", h.Name(), b.name, err)

				// So empty response
				responses <- &http.Response{}
			} else {
				if resp.StatusCode/100 == 5 {
					// HTTP error
					log.Printf("5xx response for relay %q backend %q: %v", h.Name(), b.name, resp.StatusCode)
				}

				// Get response
				responses <- resp
			}
		}()
	}

	// Wait for requests
	go func() {
		wg.Wait()
		close(responses)
	}()

	var errResponse *responseData
	for resp := range responses {
		switch resp.StatusCode / 100 {
		case 2:
			w.WriteHeader(http.StatusNoContent)
			return

		case 4:
			// User error
			resp.Write(w)
			return

		default:
			// Hold on to one of the responses to return back to the client
			errResponse = nil
		}
	}

	// No successful writes
	if errResponse == nil {
		// Failed to make any valid request...
		jsonResponse(w, response{http.StatusServiceUnavailable, "unable to forward query"})
		return
	}
}

func (h *HTTP) handleFlush(w http.ResponseWriter, r *http.Request, start time.Time) {
	if h.log {
		h.logger.Println("Flushing buffers...")
	}

	for _, b := range h.backends {
		r := b.getRetryBuffer()

		if r != nil {
			if h.log {
				h.logger.Println("Flushing " + b.name)
			} else {
				h.logger.Println("NOT flushing " + b.name + " (is empty)")
			}

			r.empty()
		}
	}

	jsonResponse(w, response{http.StatusOK, http.StatusText(http.StatusOK)})
}

func (h *HTTP) handleStandard(w http.ResponseWriter, r *http.Request, start time.Time) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
		} else {
			jsonResponse(w, response{http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)})
			return
		}
	}

	queryParams := r.URL.Query()
	bodyBuf := getBuf()
	_, _ = bodyBuf.ReadFrom(r.Body)

	precision := queryParams.Get("precision")
	points, err := models.ParsePointsWithPrecision(bodyBuf.Bytes(), start, precision)
	if err != nil {
		putBuf(bodyBuf)
		jsonResponse(w, response{http.StatusBadRequest, "unable to parse points"})
		return
	}

	outBuf := getBuf()
	for _, p := range points {
		// Those two functions never return any errors, let's just ignore the return value
		_, _ = outBuf.WriteString(p.PrecisionString(precision))
		_ = outBuf.WriteByte('\n')
	}

	// done with the input points
	putBuf(bodyBuf)

	// normalize query string
	query := queryParams.Encode()

	outBytes := outBuf.Bytes()

	// check for authorization performed via the header
	authHeader := r.Header.Get("Authorization")

	var wg sync.WaitGroup
	wg.Add(len(h.backends))

	var responses = make(chan *responseData, len(h.backends))

	for _, b := range h.backends {
		b := b
		if b.inputType != config.TypeInfluxdb {
			wg.Done()
			continue
		}

		go func() {
			defer wg.Done()
			resp, err := b.post(outBytes, query, authHeader)
			if err != nil {
				log.Printf("Problem posting to relay %q backend %q: %v", h.Name(), b.name, err)
				if h.log {
					h.logger.Printf("Content: %s", bodyBuf.String())
				}

				responses <- &responseData{}
			} else {
				if resp.StatusCode/100 == 5 {
					log.Printf("5xx response for relay %q backend %q: %v", h.Name(), b.name, resp.StatusCode)
				}
				responses <- resp
			}
		}()
	}

	go func() {
		wg.Wait()
		close(responses)
		putBuf(outBuf)
	}()

	var errResponse *responseData

	w.Header().Set("Content-Type", "text/plain")

	for resp := range responses {

		switch resp.StatusCode / 100 {
		case 2:
			// Status accepted means buffering,
			if resp.StatusCode == http.StatusAccepted {
				if h.log {
					h.logger.Printf("Could not reach relay %q, buffering...", h.Name())
				}
				w.WriteHeader(http.StatusAccepted)
				return
			}

			w.WriteHeader(http.StatusNoContent)
			return

		case 4:
			// User error
			resp.Write(w)
			return

		default:
			// Hold on to one of the responses to return back to the client
			errResponse = nil
		}
	}

	// No successful writes
	if errResponse == nil {
		// Failed to make any valid request...
		jsonResponse(w, response{http.StatusServiceUnavailable, "unable to write points"})
		return
	}
}

func (h *HTTP) handleProm(w http.ResponseWriter, r *http.Request, _ time.Time) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
		} else {
			jsonResponse(w, response{http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)})
			return
		}
	}

	authHeader := r.Header.Get("Authorization")

	bodyBuf := getBuf()
	_, _ = bodyBuf.ReadFrom(r.Body)

	outBytes := bodyBuf.Bytes()

	var wg sync.WaitGroup
	wg.Add(len(h.backends))

	var responses = make(chan *responseData, len(h.backends))

	for _, b := range h.backends {
		b := b
		if b.inputType != config.TypePrometheus {
			wg.Done()
			continue
		}

		go func() {
			defer wg.Done()
			resp, err := b.post(outBytes, r.URL.RawQuery, authHeader)
			if err != nil {
				log.Printf("Problem posting to relay %q backend %q: %v", h.Name(), b.name, err)

				responses <- &responseData{}
			} else {
				if resp.StatusCode/100 == 5 {
					log.Printf("5xx response for relay %q backend %q: %v", h.Name(), b.name, resp.StatusCode)
				}

				responses <- resp
			}
		}()
	}

	go func() {
		wg.Wait()
		close(responses)
		putBuf(bodyBuf)
	}()

	var errResponse *responseData

	w.Header().Set("Content-Type", "text/plain")

	for resp := range responses {

		switch resp.StatusCode / 100 {
		case 2:
			// Status accepted means buffering,
			if resp.StatusCode == http.StatusAccepted {
				if h.log {
					h.logger.Printf("Could not reach relay %q, buffering...", h.Name())
				}
				w.WriteHeader(http.StatusAccepted)
				return
			}

			w.WriteHeader(http.StatusNoContent)
			return

		case 4:
			// User error
			resp.Write(w)
			return

		default:
			// Hold on to one of the responses to return back to the client
			errResponse = nil
		}
	}

	// No successful writes
	if errResponse == nil {
		// Failed to make any valid request...
		jsonResponse(w, response{http.StatusServiceUnavailable, "unable to write points"})
		return
	}
}
