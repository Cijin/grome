package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type gromeURL struct {
	URL *url.URL
}

type response struct {
	proto   string
	status  string
	headers map[string]string
	content string
}

func New(rawURL string) (*gromeURL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if parsedURL.Scheme != "http" {
		return nil, errors.New("grome browser only supports http URLs")
	}

	return &gromeURL{parsedURL}, nil
}

func (g *gromeURL) Request() (*response, error) {
	conn, err := net.Dial("tcp", net.JoinHostPort(g.URL.Host, "80"))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("%s %s HTTP/1.0\r\n", http.MethodGet, g.URL.Path))
	b.WriteString(fmt.Sprintf("Host: %s\r\n", g.URL.Host))
	b.WriteString("\r\n")
	_, err = conn.Write(b.Bytes())

	b.Reset()
	_, err = io.Copy(&b, conn)

	var res response
	resScanner := bufio.NewScanner(&b)
	resScanner.Scan()
	statusLine := resScanner.Text()
	proto, status, ok := strings.Cut(statusLine, " ")
	if !ok {
		return nil, fmt.Errorf("malformed HTTP response %s", statusLine)
	}

	res.proto = proto
	res.status = status

	headers := make(map[string]string)
	for resScanner.Scan() {
		line := resScanner.Text()
		if line == "\r\n" {
			break
		}

		header, value, _ := strings.Cut(line, ":")
		headers[strings.ToLower(header)] = strings.TrimSpace(value)
	}
	if err := resScanner.Err(); err != nil {
		return nil, err
	}

	if value, ok := headers["transfer-encoding"]; ok {
		return nil, fmt.Errorf("unexpected 'transfer-encoding=%s' header present", value)
	}

	if value, ok := headers["content-encoding"]; ok {
		return nil, fmt.Errorf("unexpected 'content-encoding=%s' header present", value)
	}

	res.headers = headers

	var content strings.Builder
	for resScanner.Scan() {
		fmt.Fprint(&content, resScanner.Text())
	}
	if err := resScanner.Err(); err != nil {
		return nil, err
	}

	res.content = content.String()

	return &res, nil
}
