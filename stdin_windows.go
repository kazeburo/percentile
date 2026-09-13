package main

import "os"

func openStdin() (*os.File, error) {
	return os.Stdin, nil
}
