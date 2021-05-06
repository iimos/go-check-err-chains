package bbb

import "fmt"

func SubPkgFunction() error {
	return fmt.Errorf("aaa/bbb.SubPkgFunction: err")
}
