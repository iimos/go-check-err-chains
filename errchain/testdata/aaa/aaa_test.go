package aaa

import "errors"

func FuncInTest() error {
	return errors.New("tests should be skipped")
}
