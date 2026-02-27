package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmigrator/internal/metrics"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration progress and replication lag",
	Long:  `Status reports the current phase, LSN position, and replication lag.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		snap, err := metrics.ReadStateFile()
		if err != nil {
			fmt.Println("No migration state found. Is a migration running?")
			fmt.Printf("  (error: %v)\n", err)
			return nil
		}

		age := time.Since(snap.Timestamp)
		stale := ""
		if age > 10*time.Second {
			stale = fmt.Sprintf(" (stale — %s ago)", age.Truncate(time.Second))
		}

		fmt.Printf("Phase:        %s%s\n", snap.Phase, stale)
		fmt.Printf("Elapsed:      %.0fs\n", snap.ElapsedSec)
		fmt.Printf("Applied LSN:  %s\n", snap.AppliedLSN)
		fmt.Printf("Confirmed LSN: %s\n", snap.ConfirmedLSN)
		fmt.Printf("Lag:          %s\n", snap.LagFormatted)
		fmt.Printf("Tables:       %d/%d copied\n", snap.TablesCopied, snap.TablesTotal)
		fmt.Printf("Throughput:   %.0f rows/s, %.0f bytes/s\n", snap.RowsPerSec, snap.BytesPerSec)
		fmt.Printf("Total:        %d rows, %d bytes\n", snap.TotalRows, snap.TotalBytes)

		if snap.ErrorCount > 0 {
			fmt.Printf("Errors:       %d (last: %s)\n", snap.ErrorCount, snap.LastError)
		}

		if len(snap.Tables) > 0 {
			fmt.Println("\nTables:")
			for _, t := range snap.Tables {
				fmt.Printf("  %s.%-30s %s  %5.1f%%  (%d/%d rows)\n",
					t.Schema, t.Name, t.Status, t.Percent, t.RowsCopied, t.RowsTotal)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
