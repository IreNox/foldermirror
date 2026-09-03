package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/local/foldermirror/internal/app"
)

func main() {
	var opts app.Options
	flag.StringVar(&opts.Source, "source", "", "source directory")
	flag.StringVar(&opts.Target, "target", "", "mirror directory (same filesystem as source)")
	flag.StringVar(&opts.Listen, "listen", "127.0.0.1:8787", "HTTP listen address")
	flag.StringVar(&opts.StateFile, "state", "", "state file (default: <target>/.foldermirror.json)")
	flag.Parse()

	if opts.Source == "" || opts.Target == "" {
		fmt.Fprintln(os.Stderr, "usage: foldermirror -source PATH -target PATH [-listen 127.0.0.1:8787]")
		os.Exit(2)
	}
	if err := app.Run(opts); err != nil {
		log.Fatal(err)
	}
}
