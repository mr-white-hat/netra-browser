package main

import (
	"flag"
	"fmt"
	"os"
)

const Version = "0.0.1-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "netra-browser: not yet implemented; see --version")
	os.Exit(1)
}
