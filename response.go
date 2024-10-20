package main

import (
	"fmt"
	"strings"
)

type response struct {
	proto   string
	status  string
	headers map[string]string
	content string
}

func (r *response) String() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Protocol: %s\n", r.proto))
	b.WriteString(fmt.Sprintf("Status: %s\n", r.status))
	b.WriteString("Headers:\n")

	for key, value := range r.headers {
		b.WriteString(fmt.Sprintf("\t%s: %s\n", key, value))
	}

	b.WriteString(fmt.Sprintf("Content:\n %s", r.content))

	return b.String()
}

func (r *response) Show() string {
	var b strings.Builder

	var inTag bool
	for _, c := range r.content {
		if c == '<' {
			inTag = true
		} else if c == '>' {
			inTag = false
		} else if !inTag {
			b.WriteRune(c)
		}
	}

	return b.String()
}
