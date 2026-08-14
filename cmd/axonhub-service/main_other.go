//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("axonhub-service is only supported on Windows")
}
