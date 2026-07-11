package app

import (
	"fmt"
	"io"

	"memento-mcp/internal/feedback"
)

func runFeedbackCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("expected status, export, or delete")
	}
	cfg := feedback.ConfigFromEnv()
	store, err := feedback.NewStore(cfg)
	if err != nil {
		return err
	}

	switch args[0] {
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("status accepts no arguments")
		}
		events, err := store.ReadEvents()
		if err != nil {
			return err
		}
		state := "disabled"
		if cfg.Enabled {
			state = "enabled"
		}
		_, err = fmt.Fprintf(stdout, "feedback: %s\nstorage: %s\nevents: %d\nnetwork: never\n", state, store.Path(), len(events))
		return err
	case "export":
		if len(args) == 1 {
			return store.WriteExport(stdout)
		}
		if len(args) == 2 && args[1] == "--evaluation" {
			return store.WriteEvaluationSupplement(stdout)
		}
		return fmt.Errorf("export accepts only optional --evaluation")
	case "delete":
		if len(args) != 2 || args[1] != "--confirm" {
			return fmt.Errorf("delete requires --confirm")
		}
		if err := store.Delete(); err != nil {
			return err
		}
		_, err := fmt.Fprintln(stdout, "deleted local feedback events")
		return err
	default:
		return fmt.Errorf("unknown command %q; expected status, export, or delete", args[0])
	}
}
