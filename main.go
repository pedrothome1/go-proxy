package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

var logsDir = path.Join(".", "logs")

var portFlag = flag.Int("p", 8080, "The TCP port to bind the server to")
var forwardAddrFlag = flag.String("addr", "", "The server address (scheme://host) to forward the request to")

type logEntry struct {
	timestamp time.Time
	message   *rawHTTPMessage
}

func main() {
	flag.Parse()

	port := *portFlag
	forwardAddr := strings.TrimSuffix(*forwardAddrFlag, "/")

	ensurePortAvailable(port)
	ensureForwardURLValid(forwardAddr)

	logChan := make(chan logEntry, 2)

	go startLoggerAgent(forwardAddr, logChan)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := writeRequest(r, forwardAddr, logChan)

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}

		writeResponse(w, res, logChan)
	})

	log.Printf("Starting server on port %d\n\n", port)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(port), nil))
}

func ensureForwardURLValid(forwardAddr string) {
	forwardURL, err := url.Parse(forwardAddr)
	if err != nil {
		log.Fatal("The address must be a valid URL")
	}

	if forwardURL.Scheme != "http" && forwardURL.Scheme != "https" {
		log.Fatal("The scheme must be http or https")
	}

	if forwardAddr != forwardURL.Scheme+"://"+forwardURL.Host {
		log.Fatal("The address must be a valid HTTP URL of type scheme://host")
	}
}

func ensurePortAvailable(port int) {
	probeTCPListener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		log.Fatalf("Can't listen on port %d: %v", port, err)
	}

	_ = probeTCPListener.Close()
}

func startLoggerAgent(fileName string, logChan chan logEntry) {
	logFile := openLogFile(fileName)
	logger := log.New(logFile, "", 0)

	var reqTimestamp time.Time

	for {
		entry, ok := <-logChan

		if !ok {
			logFile.Close()

			break
		}

		logger.Println("==> " + entry.timestamp.Local().Format("02/01/2006 15:04:05"))
		logger.Println(rawMessage(entry.message))

		if entry.message.IsRequest {
			reqTimestamp = entry.timestamp
		} else {
			logger.Printf("==> Elapsed: %s\n\n", entry.timestamp.Sub(reqTimestamp))
		}
	}
}

func writeRequest(r *http.Request, forwardAddr string, logChan chan logEntry) *http.Request {
	urlPath := strings.TrimPrefix(r.URL.EscapedPath(), "/")

	reqURL, err := url.Parse(fmt.Sprintf("%s/%s?%s#%s", forwardAddr, urlPath, r.URL.RawQuery, r.URL.EscapedFragment()))
	if err != nil {
		log.Fatal(err)
	}

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Fatal(err)
	}

	req, err := http.NewRequest(r.Method, reqURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		log.Fatal(err)
	}

	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	logChan <- logEntry{timestamp: time.Now(), message: newRawHTTPRequest(req, reqBody)}

	return req
}

func writeResponse(w http.ResponseWriter, res *http.Response, logChan chan logEntry) {
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	logChan <- logEntry{timestamp: time.Now(), message: newRawHTTPResponse(res, resBody)}

	for key, values := range res.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(res.StatusCode)

	_, err = w.Write(resBody)
	if err != nil {
		log.Fatal(err)
	}
}

func openLogFile(fileName string) *os.File {
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		err := os.Mkdir(logsDir, 0755)
		if err != nil {
			log.Fatal(err)
		}
	}

	logFile, err := os.OpenFile(logFilePath(fileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}

	return logFile
}

func logFilePath(forwardAddr string) string {
	forwardURL, err := url.Parse(forwardAddr)
	if err != nil {
		log.Fatal(err)
	}

	return path.Join(logsDir, strings.ReplaceAll(forwardURL.Host, ":", "."))
}

type rawHTTPMessage struct {
	IsRequest bool
	Method    string
	Path      string
	Proto     string
	Status    string
	Header    http.Header
	Body      []byte
}

func newRawHTTPRequest(r *http.Request, rBody []byte) *rawHTTPMessage {
	return &rawHTTPMessage{
		IsRequest: true,
		Method:    r.Method,
		Path:      r.URL.Path,
		Proto:     r.Proto,
		Status:    "",
		Header:    r.Header,
		Body:      rBody,
	}
}

func newRawHTTPResponse(r *http.Response, rBody []byte) *rawHTTPMessage {
	return &rawHTTPMessage{
		IsRequest: false,
		Method:    "",
		Path:      "",
		Proto:     r.Proto,
		Status:    r.Status,
		Header:    r.Header,
		Body:      rBody,
	}
}

func rawMessage(msg *rawHTTPMessage) string {
	if msg.IsRequest {
		return rawRequestMessage(msg)
	}

	return rawResponseMessage(msg)
}

func rawRequestMessage(req *rawHTTPMessage) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s %s %s\r\n", req.Method, req.Path, req.Proto))
	sb.WriteString(rawHeadersAndBody(req))

	return sb.String()
}

func rawResponseMessage(res *rawHTTPMessage) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s %s\r\n", res.Proto, res.Status))
	sb.WriteString(rawHeadersAndBody(res))

	return sb.String()
}

func rawHeadersAndBody(msg *rawHTTPMessage) string {
	var sb strings.Builder

	headerKeys := make([]string, len(msg.Header))

	i := 0
	for k := range msg.Header {
		headerKeys[i] = k
		i++
	}

	sort.Strings(headerKeys)

	for _, key := range headerKeys {
		values := msg.Header[key]

		for _, value := range values {
			sb.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
		}
	}

	sb.WriteString(fmt.Sprintf("\r\n%s\r\n", msg.Body))

	return sb.String()
}
