package main

import (
	"fmt"
	"io"
	"strings"
)

type response struct {
	proto        string
	conn         io.ReadWriteCloser
	status       int
	statusString string
	headers      map[string]string
	content      string
	keepalive    bool
	viewsource   bool
}

func (r *response) String() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Protocol: %s\n", r.proto))
	b.WriteString(fmt.Sprintf("Status: %s\n", r.statusString))
	b.WriteString("Headers:\n")

	for key, value := range r.headers {
		b.WriteString(fmt.Sprintf("\t%s: %s\n", key, value))
	}

	b.WriteString(fmt.Sprintf("Content:\n %s", r.content))

	return b.String()
}

func (r *response) Show() string {
	if r.viewsource {
		return r.content
	}

	var b strings.Builder
	var inTag bool
	for i := 0; i < len(r.content); i++ {
		c := r.content[i]
		if c == '<' {
			inTag = true
		} else if c == '>' {
			inTag = false
		} else if !inTag {
			if c == '&' {
				if i+3 != len(r.content) {
					nextByte := r.content[i+1]
					var printByte byte
					if nextByte == 'l' {
						printByte = '<'
					} else if nextByte == 'g' {
						printByte = '>'
					} else {
						b.WriteByte(c)
						continue
					}

					nextByte = r.content[i+2]
					if nextByte != 't' {
						b.WriteByte(c)
						continue
					}

					nextByte = r.content[i+3]
					if nextByte != ';' {
						b.WriteByte(c)
						continue
					}
					b.WriteByte(printByte)
					i += 3
					continue
				}
			}
			b.WriteByte(c)
		}
	}

	return b.String()
}
