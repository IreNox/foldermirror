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
	flag.StringVar(&opts.Source, "storage", "", "storage directory")
	flag.StringVar(&opts.Source, "source", "", "alias for -storage")
	flag.StringVar(&opts.Target, "mirror", "", "mirror directory (same filesystem as storage)")
	flag.StringVar(&opts.Target, "target", "", "alias for -mirror")
	flag.StringVar(&opts.Imports, "imports", "", "optional root containing folders to collect files from")
	flag.StringVar(&opts.Listen, "listen", "127.0.0.1:8787", "HTTP listen address")
	flag.StringVar(&opts.StateFile, "state", "", "state file (default: <mirror>/.foldermirror.json)")
	flag.Parse()

	if opts.Source == "" || opts.Target == "" {
		fmt.Fprintln(os.Stderr, "usage: foldermirror -storage PATH -mirror PATH [-imports PATH] [-listen 127.0.0.1:8787]")
		os.Exit(2)
	}
	if err := app.Run(opts); err != nil {
		log.Fatal(err)
	}
}
