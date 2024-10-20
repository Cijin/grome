package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

var supportedSchemes = []string{"https", "http"}

type gromeURL struct {
	URL *url.URL
}

func New(rawURL string) (*gromeURL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if !slices.Contains(supportedSchemes, parsedURL.Scheme) {
		return nil, fmt.Errorf("unsupported url scheme:%s", parsedURL.Scheme)
	}

	return &gromeURL{parsedURL}, nil
}

func (g *gromeURL) Request() (*response, error) {
	var conn io.ReadWriteCloser
	if g.URL.Scheme == "https" {
		c, err := net.Dial("tcp", net.JoinHostPort(g.URL.Host, "443"))
		if err != nil {
			return nil, err
		}

		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
		conn = tls.Client(c, &tls.Config{RootCAs: roots, ServerName: g.URL.Host})
	} else {
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
	}
	defer conn.Close()

	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("%s %s HTTP/1.0\r\n", http.MethodGet, g.URL.Path))
	b.WriteString(fmt.Sprintf("Host: %s\r\n", g.URL.Host))
	b.WriteString("\r\n")
	_, err := conn.Write(b.Bytes())
	if err != nil {
		return nil, err
	}

	b.Reset()
	_, err = io.Copy(&b, conn)

	var res response
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
