package main

import "fmt"

func main() {
	var f float64 = 1234.12345678

	fmt.Println(f)
	fmt.Println(FormatFloat(f, byte('f'), -1, 64))
}
