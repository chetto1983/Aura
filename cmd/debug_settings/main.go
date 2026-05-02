// Debug helper — dumps the settings KV table.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aura/aura/internal/settings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: debug_settings <db_path>")
		os.Exit(2)
	}
	s, err := settings.OpenStore(os.Args[1])
	if err != nil {
		fmt.Println("err:", err)
		os.Exit(1)
	}
	defer s.Close()
	all, _ := s.All(context.Background())
	for k, v := range all {
		fmt.Printf("%s = %s\n", k, v)
	}
}
