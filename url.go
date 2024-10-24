package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	userAgent           = "grome-browser"
	httpVersion         = "HTTP/1.1"
	maxAllowedRedirects = 5
)

type cached struct {
	expires  time.Time
	response *response
}

var cache = make(map[string]cached)

type gromeURL struct {
	URL           *url.URL
	viewsource    bool
	keepalive     bool
	conn          io.ReadWriteCloser
	redirectCount int
	cache         map[string]cached
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

	keepalive := true
	if parsedURL.Scheme == "file" || parsedURL.Scheme == "data" {
		keepalive = false
	}

	return &gromeURL{
		URL:           parsedURL,
		viewsource:    viewsource,
		keepalive:     keepalive,
		conn:          nil,
		redirectCount: 0,
	}, nil
}

func (g *gromeURL) Request() (*response, error) {
	var conn io.ReadWriteCloser
	var res response
	res.viewsource = g.viewsource
	res.keepalive = g.keepalive

	if g.keepalive && g.conn != nil {
		conn = g.conn
	}

	if conn == nil {
		switch g.URL.Scheme {
		case "https":
			var err error
			conn, err = getHttpsConn(g.URL.Host, "443")
			if err != nil {
				return nil, err
			}

		case "http":
			var err error
			conn, err = getHttpConn(g.URL.Host, "80")
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
	}
	res.conn = conn

	err := writeRequest(conn, g.URL, g.keepalive)
	if err != nil {
		return nil, err
	}

	resReader := bufio.NewReader(conn)

	statusElements, err := getStatusElements(resReader)
	if err != nil {
		return nil, err
	}

	proto := statusElements[0]
	statusString := statusElements[1]
	status, err := getStatus(statusString)
	if err != nil {
		return nil, err
	}

	res.proto = proto
	res.statusString = statusString
	res.status = status

	headers, err := getHeaders(resReader)
	if err != nil {
		return nil, err
	}
	res.headers = headers

	cacheDirective, ok := headers["cache-control"]
	cacheable := ok && strings.Contains(cacheDirective, "max-age") && status == 200
	if cacheable {
		cached, ok := cache[g.URL.String()]
		if ok {
			if cached.expires.Before(time.Now()) {
				delete(cache, g.URL.String())
			} else {
				return cached.response, nil
			}
		}
	}

	if status >= 300 && status < 400 {
		for {
			g.redirectCount += 1
			if g.redirectCount >= maxAllowedRedirects {
				return nil, errors.New("redirect loop has exceeded max allowed")
			}

			r, ok := headers["location"]
			if !ok {
				return nil, errors.New("redirect response is missing location header")
			}

			if r[0] == '/' {
				temp := g.URL.Scheme + "://" + g.URL.Host
				r = temp + r
			}

			redirectURL, err := url.Parse(r)
			if err != nil {
				return nil, fmt.Errorf("unable to parse redirect url:%v", err)
			}

			isCrossOriginRedirection := redirectURL.Host != g.URL.Host
			if isCrossOriginRedirection {
				if redirectURL.Scheme == "http" {
					conn, err = getHttpConn(redirectURL.Host, "80")
					if err != nil {
						return nil, fmt.Errorf("unable to establish http conn for redirectURL:%s, err:%v", redirectURL, err)
					}
				} else if redirectURL.Scheme == "https" {
					conn, err = getHttpsConn(redirectURL.Host, "443")
					if err != nil {
						return nil, fmt.Errorf("unable to establish https conn for redirectURL:%s, err:%v", redirectURL, err)
					}
				} else {
					return nil, fmt.Errorf("invalid scheme '%s' for redirectURL:%s", redirectURL.Scheme, redirectURL)
				}
			}

			// close connections for cross origin requests
			err = writeRequest(conn, redirectURL, !isCrossOriginRedirection)
			if err != nil {
				return nil, fmt.Errorf("failed to write redirect request:%v", err)
			}

			resReader = bufio.NewReader(conn)
			statusElements, err := getStatusElements(resReader)
			if err != nil {
				return nil, fmt.Errorf("redirect status err:%v", err)
			}

			status, err := getStatus(statusElements[1])
			if err != nil {
				return nil, err
			}

			headers, err = getHeaders(resReader)
			if err != nil {
				return nil, err
			}

			if status >= 300 && status < 400 {
				continue
			}
			break
		}
	}

	contentEncoding, ok := headers["content-encoding"]
	isGzipped := ok && contentEncoding == "gzip"
	if g.keepalive {
		s, ok := headers["content-length"]
		if !ok {
			return nil, errors.New("response header is missing content-length for a keep alive request")
		}

		contentLength, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("content length '%s' is not valid", s)
		}

		content := make([]byte, contentLength)
		_, err = io.ReadFull(resReader, content)
		if err != nil {
			return nil, fmt.Errorf("error reading content from response reader:%v", err)
		}

		if isGzipped {
			content, err = getUncompressedContent(content)
			if err != nil {
				return nil, err
			}

			res.content = string(content)
		} else {
			if isGzipped {
				content, err = getUncompressedContent(content)
				if err != nil {
					return nil, err
				}
			}
			res.content = string(content)
		}
	} else {
		res.content, _ = resReader.ReadString(0)
	}

	if cacheable {
		// max-age=xxx
		_, maxAgeString, ok := strings.Cut(cacheDirective, "=")
		if !ok {
			fmt.Printf("cache directive '%s' is not cacheable", cacheDirective)
		}
		maxAge, err := strconv.Atoi(maxAgeString)
		if err != nil {
			fmt.Printf("Max age value '%s' is not valid", cacheDirective)
		} else {
			cache[g.URL.String()] = cached{expires: time.Now().Add(time.Duration(maxAge) * time.Second), response: &res}
		}
	}

	return &res, nil
}

func getUncompressedContent(c []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(c))
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	uncompressed, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}

	return uncompressed, nil
}

func getHttpConn(host, port string) (net.Conn, error) {
	if strings.Contains(host, ":") {
		conn, err := net.Dial("tcp", host)
		if err != nil {
			return nil, err
		}

		return conn, nil
	}

	conn, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func getHttpsConn(url, port string) (*tls.Conn, error) {
	c, err := getHttpConn(url, port)
	if err != nil {
		return nil, err
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	return tls.Client(c, &tls.Config{RootCAs: roots, ServerName: url}), nil
}

func getHeaders(r *bufio.Reader) (map[string]string, error) {
	headers := make(map[string]string)
	for {
		line, _ := r.ReadString('\n')
		if line == "\r\n" {
			break
		}
		header, value, _ := strings.Cut(line, ":")
		headers[strings.ToLower(header)] = strings.TrimSpace(value)
	}

	if value, ok := headers["transfer-encoding"]; ok {
		return nil, fmt.Errorf("unexpected 'transfer-encoding=%s' header present", value)
	}

	return headers, nil
}

func getStatusElements(r *bufio.Reader) ([]string, error) {
	statusLine, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}

	statusElements := strings.SplitN(statusLine, " ", 2)
	if len(statusElements) != 2 {
		return nil, fmt.Errorf("malformed status response %s", statusLine)
	}

	return statusElements, nil
}

func writeRequest(conn io.Writer, u *url.URL, keepalive bool) error {
	_, err := conn.Write(getRequestBytes(u, keepalive))
	return err
}

func getRequestBytes(u *url.URL, keepalive bool) []byte {
	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("%s %s %s\r\n", http.MethodGet, u.Path, httpVersion))
	b.WriteString(defaultHeaders(u.Host, keepalive))
	b.WriteString("\r\n")

	return b.Bytes()
}

func getStatus(statusString string) (int, error) {
	statusValue, _, ok := strings.Cut(statusString, " ")
	if !ok {
		return 0, fmt.Errorf("unable to retrieve status from status string: %s", statusString)
	}
	status, err := strconv.Atoi(statusValue)
	if err != nil {
		return 0, fmt.Errorf("status %s is invalid", statusValue)
	}

	return status, nil
}

func defaultHeaders(host string, keepalive bool) string {
	headers := make(map[string]string)
	headers["Host"] = host
	headers["Accept-Encoding"] = "gzip"
	headers["User-Agent"] = userAgent
	if keepalive {
		headers["Connection"] = "keep-alive"
	} else {
		headers["Connection"] = "close"
	}

	return addHeaders(headers)
}

func addHeaders(h map[string]string) string {
	var b bytes.Buffer
	for k, v := range h {
		b.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	return b.String()
}
