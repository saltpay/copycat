package input

import "fmt"

// ClearScreen clears the terminal screen and moves the cursor to the top-left.
func ClearScreen() {
	fmt.Print("\033[2J\033[H")
}
