package main

import (
	"flag"
	"fmt"
	"github.com/nokia/ntt/ttcn3/syntax"
	"os"
)

var (
	trace = flag.Bool("t", false, "Trace parser")
)

func worker(id int, jobs <-chan string, results chan<- error) {
	for j := range jobs {
		results <- parse(j)
	}
}

func main() {
	flag.Parse()

	ret := 0

	jobs := make(chan string)
	results := make(chan error)

	for w := 1; w <= len(flag.Args()); w++ {
		go worker(w, jobs, results)
	}

	for _, v := range flag.Args() {
		jobs <- v
	}
	close(jobs)

	for range flag.Args() {
		err := <-results
		if err != nil {
			ret = 1
		}
	}
	os.Exit(ret)
}

func parse(file string) error {
	mode := syntax.Mode(syntax.Trace)
	if !*trace {
		mode = 0
	}

	_, err := syntax.ParseModule(syntax.NewFileSet(), file, nil, mode, func(pos syntax.Position, msg string) {
		fmt.Fprintf(os.Stderr, "%s: error: %s\n", pos, msg)
	})

	return err
}
