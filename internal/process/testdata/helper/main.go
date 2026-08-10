// Command helper is a tiny dependency-free subprocess used by the process
// package tests. Behaviours are selected by the first argument.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	behaviour := "print"
	if len(os.Args) > 1 {
		behaviour = os.Args[1]
	}
	args := os.Args[2:]

	switch behaviour {
	case "print":
		n := 5
		if len(args) > 0 {
			n, _ = strconv.Atoi(args[0])
		}
		for i := 0; i < n; i++ {
			fmt.Printf("out-line-%d\n", i)
			time.Sleep(20 * time.Millisecond)
		}
		fmt.Println("READY")
		for {
			time.Sleep(time.Hour)
		}
	case "once":
		for i := 0; i < 3; i++ {
			fmt.Printf("once-line-%d\n", i)
			time.Sleep(5 * time.Millisecond)
		}
	case "burst":
		n := 50
		if len(args) > 0 {
			n, _ = strconv.Atoi(args[0])
		}
		for i := 0; i < n; i++ {
			fmt.Printf("burst-line-%d\n", i)
		}
	case "longline":
		n := 2000
		if len(args) > 0 {
			n, _ = strconv.Atoi(args[0])
		}
		fmt.Printf("%s\n", strings.Repeat("x", n))
	case "fixedlines":
		for _, a := range args {
			n, _ := strconv.Atoi(a)
			fmt.Printf("%s\n", strings.Repeat("x", n))
		}
	case "stderr-exit":
		fmt.Fprintln(os.Stderr, "boom-on-stderr")
		fmt.Fprintln(os.Stderr, "second-stderr-line")
		os.Exit(1)
	case "stdin-echo":
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			fmt.Printf("got: %s\n", sc.Text())
		}
		fmt.Println("stdin-closed")
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("RUNNING")
		for {
			time.Sleep(time.Hour)
		}
	case "graceful":
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		fmt.Println("RUNNING")
		s := <-sig
		fmt.Printf("recv-%s\n", s)
	case "env":
		name := "PATH"
		if len(args) > 0 {
			name = args[0]
		}
		fmt.Printf("%s=%s\n", name, os.Getenv(name))
	case "spawn":
		cmd := exec.Command(os.Args[0], "grandchild")
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Printf("CHILD=%d\n", cmd.Process.Pid)
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("RUNNING")
		for {
			time.Sleep(time.Hour)
		}
	case "grandchild":
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("RUNNING")
		for {
			time.Sleep(time.Hour)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown behaviour %q\n", behaviour)
		os.Exit(3)
	}
}
