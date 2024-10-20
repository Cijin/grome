package main

import (
	"fmt"
	"os"
)

func main() {
	g, err := New("view-source:http://example.org/")
	if err != nil {
		fmt.Println("error parsing url:", err)
		os.Exit(1)
	}

	res, err := g.Request()
	if err != nil {
		fmt.Println("unable to get response:", err)
		os.Exit(1)
	}

	view := res.Show()
	fmt.Println(view)
}
