//go:build wasip1

package main

import "errors"

func measure([]string) error { return errors.New("measurement driver requires native Go") }
