package main

import (
	"log"
	"github.com/Linux-DEX/azstorecli/pkg/ui"
)

func main() {
	if err := ui.RunApp(); err != nil {
		log.Fatal(err)
	}
}
