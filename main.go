package main

import "fmt"

func main() {
	g, err := New("http://example.org/index.html")
	if err != nil {
		panic(err)
	}

	res, _ := g.Request()
	fmt.Println(res)
}
