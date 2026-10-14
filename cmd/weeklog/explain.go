package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/store"
)

func newExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <work-item-or-event>",
		Short: "Print why events were linked",
		Long: `Print one work item and the stored reason for each of its events.

A bare number or item:ID selects a work item. Work item ids are rewritten
each time the linker runs, so SOURCE/SOURCE_ID and event:ID stay valid across
link runs. SOURCE/SOURCE_ID splits on the first slash. The source id may
itself contain slashes, as in github/acme/otto#12. event:ID selects the
event row id.

The events of that work item are printed with the reason stored for each
link. An event that is not in a work item is printed as not linked.

Reasons come from the linker passes and from weeklog link / weeklog unlink.
See docs/LINKING.md.

The database is the same file weeklog collect uses. --db selects another file.`,
		Example: `  weeklog explain 1
  weeklog explain item:1
  weeklog explain event:4
  weeklog explain linear/GEN-301
  weeklog explain git/0123456789abcdef0123456789abcdef01234567`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         runExplain,
	}
	cmd.Flags().String("db", "", "SQLite database (default is the weeklog data directory)")
	return cmd
}

func runExplain(cmd *cobra.Command, args []string) error {
	target, err := parseExplainArg(args[0])
	if err != nil {
		return err
	}
	_, loc, _, err := loadLinkConfig(cmd)
	if err != nil {
		return err
	}
	dbFlag, err := cmd.Flags().GetString("db")
	if err != nil {
		return err
	}
	dbPath, err := resolveDBPath(dbFlag)
	if err != nil {
		return err
	}
	ctx := commandContext(cmd)
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	events, err := st.ListEvents(ctx)
	if err != nil {
		return err
	}
	byID := make(map[int64]store.Record, len(events))
	for _, ev := range events {
		byID[ev.ID] = ev
	}
	items, err := st.ListWorkItems(ctx)
	if err != nil {
		return err
	}

	if target.isItem {
		for _, item := range items {
			if item.ID != target.item {
				continue
			}
			text, err := formatExplain(item, byID, loc)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		}
		return fmt.Errorf("work item %d not found", target.item)
	}

	rec, err := resolveEvent(ctx, st, target.event)
	if err != nil {
		return err
	}
	for _, item := range items {
		for _, member := range item.Events {
			if member.EventID != rec.ID {
				continue
			}
			text, err := formatExplain(item, byID, loc)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		}
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), formatUnlinked(rec, loc))
	return err
}

func formatExplain(item store.WorkItem, byID map[int64]store.Record, loc *time.Location) (string, error) {
	if loc == nil {
		loc = time.UTC
	}
	var b strings.Builder
	fmt.Fprintf(&b, "work item %d\n", item.ID)
	fmt.Fprintf(&b, "title: %s\n", oneLine(item.Title))
	fmt.Fprintf(&b, "status: %s\n", item.Status)
	for _, member := range item.Events {
		rec, ok := byID[member.EventID]
		if !ok {
			return "", fmt.Errorf("work item %d references missing event %d", item.ID, member.EventID)
		}
		writeEventBlock(&b, rec, member.Reason, loc)
	}
	return b.String(), nil
}

func formatUnlinked(rec store.Record, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	var b strings.Builder
	writeEventBlock(&b, rec, "not linked", loc)
	return b.String()
}

func writeEventBlock(b *strings.Builder, rec store.Record, reason string, loc *time.Location) {
	fmt.Fprintf(b, "\n%s/%s\n", rec.Source, rec.SourceID)
	fmt.Fprintf(b, "  id: %d\n", rec.ID)
	fmt.Fprintf(b, "  kind: %s\n", rec.Kind)
	fmt.Fprintf(b, "  ts: %s\n", rec.TS.In(loc).Format(time.RFC3339))
	if title := oneLine(rec.Title); title != "" {
		fmt.Fprintf(b, "  title: %s\n", title)
	}
	fmt.Fprintf(b, "  reason: %s\n", reason)
}

func oneLine(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r\n", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	return strings.Join(strings.Fields(text), " ")
}
