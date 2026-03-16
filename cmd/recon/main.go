package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: recon <command> [options]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  serve                          Start the HTTP server")
		fmt.Println("  sync stripe|paypal             Manually trigger sync")
		fmt.Println("  upload <file>                  Ingest a bank statement file")
		fmt.Println("  reconcile --from --to          Run reconciliation")
		fmt.Println("  report --type --from --to      Generate report")
		fmt.Println("  discrepancies --status         List discrepancies")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		fmt.Println("Starting server... (not yet implemented)")
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
