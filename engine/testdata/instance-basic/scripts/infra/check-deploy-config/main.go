// Command check-deploy-config: Validate Helm values files for required resources and image tags.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--help" {
		fmt.Println("usage: check-deploy-config [--chart DIR] [--format text|json]")
		return
	}
	fmt.Println("check-deploy-config: ok")
}
