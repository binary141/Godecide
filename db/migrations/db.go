package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	now := time.Now().Unix()

	args := os.Args

	if len(args) < 2 {
		_, _ = os.Create(fmt.Sprintf("%d.sql", now))
		return
	}

	name := strings.Join(args[1:], "_")

	_, _ = os.Create(fmt.Sprintf("%d_%s.sql", now, name))
}
