package main

import (
	"fmt"
	"os"
)

func main() {
	g, err := New("http://browser.engineering/redirect3")
	if err != nil {
		fmt.Println("error parsing url:", err)
		os.Exit(1)
	}

	res, err := g.Request()
	if err != nil {
		fmt.Println("unable to get response:", err)
		os.Exit(1)
	}
	if res.keepalive {
		defer res.conn.Close()
	}

	view := res.Show()
	fmt.Println(view)
}
