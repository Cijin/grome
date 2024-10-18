package main

import "fmt"

func main() {
	g, err := New("http://example.org/index.html")
	if err != nil {
		panic(err)
	}

	res, err := g.Request()
	fmt.Println(res.content, err)
}
