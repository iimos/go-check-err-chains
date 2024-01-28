# go-check-err-chains

The linter checks that the error text contains a prefix indicating the package/function/method where the error occurred. 

The check is only performed for exported functions.

Example:

```go
package pkg

func Get(key string) error {
    id, err := strconv.Atoi(key)
    if err != nil {
        return errors.New("pkg.Get: %w; key=%s", err, key) // good
    }
    if empty {
        return sql.ErrNoRows // bad
    }
    return nil
}
```

## Why

This linter is an attempt to bring order to the logs. 

It is often unclear from the logs where the problem occurred. 

There are already a couple of solutions to this problem, but all have significant drawbacks:

1. Use a logger that annotates entries with the file and line where the log was written. 

    The file and line number can be indicated by the logger, but this will be the place where log.Error was called, 
    not the place where the error occurred. 

    This means that errors need to be logged right where they occurred. 
    As a result, logs start to be written everywhere and with great redundancy. 

    One error generates several similar messages about the same error 
    and each message has a slightly different context and to see the full context, 
    all these entries need to be collected.

2. Use third-party libraries like [pkg/errors](https://github.com/pkg/errors) for errors. 

    There are many libraries that save the call stack when creating an error. 

    This is not a bad approach, but there are a number of downsides:
    - everywhere you need to use one specific library for errors
    - poorly combined with native [wrapping through %w](https://go.dev/blog/go1.13-errors)


At the moment, the optimal compromise seems to be to go the idiomatic way accepted in the standard library:

Examples of errors from the standard library:

- strconv.Atoi: parsing "x": invalid syntax
- plugin.Open("file.so"): realpath failed
- bytes.Buffer: too large
- open file.go: no such file or directory
- strings.Builder.Grow: negative count

It can be seen that there is no strict scheme, but a pattern can be traced: first the name of the package/method/function, then a colon and further details.

```go
package pkg

func Get(key string) error {
    id, err := strconv.Atoi(key)
    if err != nil {
        return errors.New("pkg.Get: %w; key=%s", err, key)
    }
    // ...
}
```

`pkg.Get("abc")` gives an error:
```
pkg.Get: strconv.Atoi: parsing "abc": invalid syntax; key=abc
```

This approach has several advantages:

- Well aligned with standard library errors

- Compatible with wrapping

- Writing error text is easier:

  `failed to get package cause key abc is not valid ...` vs `pkg.Get: strconv.Atoi: parsing "abc": invalid syntax`.

  The second option is easier for an engineer to read and write.

- Logs are easy to grep


The main disadvantage of the approach is that you need to monitor the consistency of the package/function name 
and error/log text, because when renaming a function, you need to remember to change all errors/logs.

This is the problem that this linter solves.
