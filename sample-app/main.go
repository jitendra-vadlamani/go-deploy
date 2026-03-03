package main
import (
"fmt"
"os"
)
func main() {
	fmt.Println("Hello from Sample App!")
	fmt.Println("APP_ENV:", os.Getenv("APP_ENV"))
	fmt.Println("DEBUG:", os.Getenv("DEBUG"))
}
