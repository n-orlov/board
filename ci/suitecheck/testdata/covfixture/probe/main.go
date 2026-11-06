// Command probe is the covfixture's stand-in for an instrumented board
// binary: a few statements, so a -cover build of it leaves counter data.
package main

import "fmt"

func main() {
	total := 0
	for i := 0; i < 3; i++ {
		total += i
	}
	fmt.Println("probe", total)
}
