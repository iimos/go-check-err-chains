package aaa

import (
	"errors"
	"fmt"
)

const pkgName = "aaa"

type Struct struct{}

func (x *Struct) Method(input string) (int, error) {
	const fn = pkgName + ".Struct" + ".Method"
	if len(input) < 1 {
		return 0, fmt.Errorf("%s: input too short, require longer than %d, input=%q", fn, 1, input)
	}
	if len(input) < 2 {
		return 0, fmt.Errorf("aaa.Struct: input too short, require longer than %d, input=%q", 2, input)
	}
	if len(input) < 3 {
		return 0, fmt.Errorf("input too short, require longer than %d, input=%q", 3, input) // want `Error message must point to the place where it had happened. Consider starting message with one of the following strings: "aaa: ", "aaa\.Struct\.Method: ", "aaa\.\(\*Struct\)\.Method: "`
	}
	if len(input) < 4 {
		return 0, fmt.Errorf("aaa.(*Struct.Method: error") // want `Error message must point to the place where it had happened. Consider starting message with one of the following strings: "aaa: ", "aaa\.Struct\.Method: ", "aaa\.\(\*Struct\)\.Method: "`
	}
	if len(input) < 5 {
		return 0, errors.New("errrrrr") // want `Error message must point to the place where it had happened. Consider starting message with one of the following strings: "aaa: ", "aaa\.Struct\.Method: ", "aaa\.\(\*Struct\)\.Method: "`
	}

	if err := fmt.Errorf("aaa: 100%%err in %q", input); err != nil {
		return 0, err
	}

	return 0, nil
}

func (x Struct) MethodWithoutPointer() error {
	return fmt.Errorf("aaa.(*Struct).MethodWithoutPointer: error") // want `Error message must point to the place where it had happened. Consider starting message with one of the following strings: "aaa: ", "aaa\.Struct\.MethodWithoutPointer: "`
}

func (x *Struct) method() (string, error) {
	return "", errors.New("private methods are allowed to return any error message")
}

func privateFunction() error {
	return errors.New("private functions are allowed to return any error message")
}

func PublicFunction() error {
	err := func() error {
		return errors.New("anonymous function err") // want `Error message must point to the place where it had happened. Consider starting message with one of the following strings: "aaa: ", "aaa\.PublicFunction: "`
	}()
	if err != nil {
		return errors.New("private functions are allowed to return any error message") // want `Error message must point to the place where it had happened. Consider starting message with one of the following strings: "aaa: ", "aaa\.PublicFunction: "`
	}
	return nil
}

var AnonymFunc = func() error {
	return errors.New("such functions are skiped for now but should be handled in future")
}

func PublicFunction2() error {
	text := "passing message via var or function call is not supported yet"
	return errors.New(text)
}

func PublicFunction3() string {
	err := errors.New("skip check if function doesn't return an error")
	return err.Error()
}
