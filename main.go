package main

import (
	"github.com/iimos/go-check-err-chains/errchain"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(errchain.Analyzer)
}
