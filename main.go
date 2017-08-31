package main

import (
	"log"

	"github.com/netlify/git-gateway/cmd"
)

func main() {
	if err := cmd.RootCommand().Execute(); err != nil {
		log.Fatal(err)
	}
}
