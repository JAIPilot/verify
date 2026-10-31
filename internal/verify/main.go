package verify

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
)

func Main(args []string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	if len(args) == 0 {
		return checkMain(ctx, nil)
	}
	switch args[0] {
	case "check":
		return checkMain(ctx, args[1:])
	case "help":
		if len(args) == 1 {
			return checkMain(ctx, []string{"--help"})
		}
	case "--version":
		if len(args) == 1 {
			fmt.Println(Version)
			return 0
		}
	case "update":
		if len(args) == 1 {
			result, err := checkAndInstallUpdate(ctx, true)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Verify update failed:", err)
				return 2
			}
			if !result.Updated {
				fmt.Println("Verify", Version, "is up to date")
			} else if result.Scheduled {
				fmt.Println("Verify", result.Version, "will be installed after this process exits; run verify again")
			} else {
				fmt.Println("Updated Verify to", result.Version+"; the next run uses it")
			}
			return 0
		}
	default:
		if strings.HasPrefix(args[0], "-") {
			return checkMain(ctx, args)
		}
	}
	fmt.Fprintln(os.Stderr, "Unknown command:", args[0])
	return 2
}
