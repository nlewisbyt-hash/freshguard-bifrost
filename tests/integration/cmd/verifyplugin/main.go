package main

import (
	"fmt"
	"os"
	"plugin"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: verifyplugin PATH")
		os.Exit(2)
	}
	loaded, err := plugin.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, symbol := range []string{"Init", "GetName", "PreRequestHook", "PreLLMHook", "PostLLMHook", "Cleanup"} {
		if _, err := loaded.Lookup(symbol); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
