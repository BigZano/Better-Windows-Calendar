package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/daemon"
	"pycalendar/internal/storage"
	"pycalendar/ui"
)

func main() {
	mode := flag.String("mode", "tray", "Run mode: tray | daemon | bar | cli")
	format := flag.String("format", "text", "Bar output format: text | json | polybar  (bar mode only)")
	maxEvents := flag.Int("max-events", 3, "Max events to show (bar mode only)")

	flag.Parse()

	// Structured logging to stderr; tray/daemon redirect to file in production.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	switch *mode {
	case "tray":
		ui.RunTray()

	case "daemon":
		if err := storage.InitDB(); err != nil {
			slog.Error("init db failed", "err", err)
			os.Exit(1)
		}
		d := daemon.New(30 * time.Second)
		d.Run()

	case "bar":
		if err := storage.InitDB(); err != nil {
			slog.Error("init db failed", "err", err)
			os.Exit(1)
		}
		events, err := api.GetUpcoming(10)
		if err != nil {
			slog.Error("get upcoming failed", "err", err)
			os.Exit(1)
		}
		switch *format {
		case "json":
			out, err := ui.FormatJSON(events, *maxEvents)
			if err != nil {
				slog.Error("format json failed", "err", err)
				os.Exit(1)
			}
			fmt.Println(out)
		case "polybar":
			fmt.Println(ui.FormatPolybar(events, *maxEvents))
		default:
			fmt.Println(ui.FormatText(events, *maxEvents))
		}

	case "cli":
		if err := storage.InitDB(); err != nil {
			slog.Error("init db failed", "err", err)
			os.Exit(1)
		}
		args := flag.Args()
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: pycalendar --mode cli <command> [flags]")
			fmt.Fprintln(os.Stderr, "commands: add, list")
			os.Exit(1)
		}
		switch args[0] {
		case "add":
			addFlags := flag.NewFlagSet("add", flag.ExitOnError)
			cliTitle := addFlags.String("title", "", "Event title")
			cliStart := addFlags.String("start", "", "Event start time YYYY-MM-DD HH:MM")
			cliNotes := addFlags.String("notes", "", "Event notes (optional)")
			cliReminder := addFlags.Int("reminder", 15, "Reminder minutes before start")
			addFlags.Parse(args[1:]) //nolint:errcheck – ExitOnError handles it

			if *cliTitle == "" || *cliStart == "" {
				fmt.Fprintln(os.Stderr, "add requires --title and --start")
				os.Exit(1)
			}
			startTime, err := time.ParseInLocation("2006-01-02 15:04", *cliStart, time.Local)
			if err != nil {
				startTime, err = time.Parse(time.RFC3339, *cliStart)
				if err != nil {
					fmt.Fprintf(os.Stderr, "invalid --start: %v\n", err)
					os.Exit(1)
				}
			}
			id, err := api.CreateEvent(*cliTitle, startTime, nil, *cliNotes, cliReminder, "", false, "Local")
			if err != nil {
				fmt.Fprintf(os.Stderr, "create event failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("created event %d\n", id)

		case "list":
			events, err := api.GetUpcoming(20)
			if err != nil {
				fmt.Fprintf(os.Stderr, "list failed: %v\n", err)
				os.Exit(1)
			}
			if len(events) == 0 {
				fmt.Println("no upcoming events")
				return
			}
			for _, e := range events {
				fmt.Printf("[%3d] %s  %s\n", e.ID, e.StartTime().Format("2006-01-02 15:04"), e.Title)
			}

		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}
