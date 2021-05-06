package main

import "errors"

func Function() error {
	return errors.New("main package is allowed to have any error messages")
}
