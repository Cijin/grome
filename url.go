package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	userAgent   = "grome-browser"
	httpVersion = "HTTP/1.1"
)

// TODO 1-6

type gromeURL struct {
	URL        *url.URL
	viewsource bool
}

func New(rawURL string) (*gromeURL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	viewsource := parsedURL.Scheme == "view-source"
	if viewsource {
		_, sourceURL, ok := strings.Cut(parsedURL.String(), ":")
		if !ok {
			return nil, errors.New("viewsource shceme url is incorrectly formatted")
		}

		parsedSourceURL, err := url.Parse(sourceURL)
		if err != nil {
			return nil, fmt.Errorf("unable to parse viewsource URL: %v", err)
		}

		parsedURL = parsedSourceURL
	}

	return &gromeURL{parsedURL, viewsource}, nil
}

func (g *gromeURL) Request() (*response, error) {
	var conn io.ReadWriteCloser
	var res response
	res.viewsource = g.viewsource

	switch g.URL.Scheme {
	case "https":
		c, err := net.Dial("tcp", net.JoinHostPort(g.URL.Host, "443"))
		if err != nil {
			return nil, err
		}

		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
		conn = tls.Client(c, &tls.Config{RootCAs: roots, ServerName: g.URL.Host})

	case "http":
		var err error
		port := "80"
		host := g.URL.Host
		if strings.Contains(g.URL.Host, ":") {
			var ok bool
			host, port, ok = strings.Cut(g.URL.Host, ":")
			if !ok {
				return nil, fmt.Errorf("unable to process host and custom port for: %s", g.URL.Host)
			}
		}

		conn, err = net.Dial("tcp", net.JoinHostPort(host, port))
		if err != nil {
			return nil, err
		}

	case "file":
		path := g.URL.Path
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		if info.IsDir() {
			files, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}

			var b strings.Builder
			for _, file := range files {
				if file.IsDir() {
					b.WriteString(fmt.Sprintf("\n%s/", file.Name()))
					continue
				}

				b.WriteString(fmt.Sprintf("\n%s", file.Name()))
			}

			res.content = b.String()
			return &res, nil
		}

		res.content = fmt.Sprintf("Name: %s\tSize: %d Bytes\n", info.Name(), info.Size())
		return &res, nil

	case "data":
		_, data, ok := strings.Cut(g.URL.String(), ":")
		if !ok {
			return nil, fmt.Errorf("%s is not a valid data URL", g.URL)
		}

		_, value, ok := strings.Cut(data, ",")
		if !ok {
			return nil, fmt.Errorf("scheme:%s, invalid format for data:%s", g.URL.Scheme, data)
		}

		res.content = value
		return &res, nil

	default:
		return nil, fmt.Errorf("grome currently does not support scheme:%s", g.URL.Scheme)
	}
	defer conn.Close()

	_, err := conn.Write(g.request())
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	_, err = io.Copy(&b, conn)

	resReader := bufio.NewReader(&b)
	statusLine, _ := resReader.ReadString('\n')
	proto, status, ok := strings.Cut(statusLine, " ")
	if !ok {
		return nil, fmt.Errorf("malformed HTTP response %s", statusLine)
	}

	res.proto = proto
	res.status = status

	headers := make(map[string]string)
	for {
		line, _ := resReader.ReadString('\n')
		if line == "\r\n" {
			break
		}
		header, value, _ := strings.Cut(line, ":")
		headers[strings.ToLower(header)] = strings.TrimSpace(value)
	}

	if value, ok := headers["transfer-encoding"]; ok {
		return nil, fmt.Errorf("unexpected 'transfer-encoding=%s' header present", value)
	}

	if value, ok := headers["content-encoding"]; ok {
		return nil, fmt.Errorf("unexpected 'content-encoding=%s' header present", value)
	}

	res.headers = headers
	res.content, _ = resReader.ReadString(0)

	return &res, nil
}

func (g *gromeURL) request() []byte {
	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("%s %s %s\r\n", http.MethodGet, g.URL.Path, httpVersion))
	b.WriteString(g.defaultHeaders())
	b.WriteString("\r\n")

	return b.Bytes()
}

func (g *gromeURL) defaultHeaders() string {
	headers := make(map[string]string)
	headers["Host"] = g.URL.Host
	headers["User-Agent"] = userAgent
	headers["Connection"] = "close"

	return addHeaders(headers)
}

func addHeaders(h map[string]string) string {
	var b bytes.Buffer
	for k, v := range h {
		b.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	return b.String()
}
