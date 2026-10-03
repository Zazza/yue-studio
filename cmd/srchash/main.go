// srchash — печатает отпечаток исходников MCP (для вшивания при make mcp).
package main

import (
	"fmt"
	"log"
	"os"

	"yue-studio/internal/buildinfo"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	h, err := buildinfo.SourceHash(root)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(h)
}
