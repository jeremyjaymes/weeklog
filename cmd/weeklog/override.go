package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/store"
)

func newLinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link <event> <event>",
		Short: "Join two events and remember the correction",
		Long: `Record that two events belong in the same work item.

Each event is SOURCE/SOURCE_ID or event:ID. A bare number is an event row
id. SOURCE/SOURCE_ID splits on the first slash, so a source id may contain
slashes. The names are the stored values and are case-sensitive.

The command appends a row to the overrides table, then runs the linker.
Overrides are applied after pass 3, so this join is kept when weeklog
link-run or weeklog collect runs again. weeklog week includes the stored
reason override:link on the two events.

The window flags match weeklog link-run. They choose the stale-branch clock
and the local day. Linking still reads every stored event.

The database is the same file weeklog collect uses. --db selects another file.`,
		Example: `  weeklog link linear/GEN-301 git/0123456789abcdef0123456789abcdef01234567
  weeklog link event:1 event:2
  weeklog link --week 2026-W41 linear/GEN-301 git/abcdef1`,
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE:         runLink,
	}
	addLinkFlags(cmd)
	return cmd
}

func newUnlinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlink <event>",
		Short: "Detach one event and remember the correction",
		Long: `Record that an event should not stay in the group the linker chose.

The event is SOURCE/SOURCE_ID or event:ID. A bare number is an event row id.
SOURCE/SOURCE_ID splits on the first slash. The name is the stored value and
is case-sensitive.

The command appends a row to the overrides table, then runs the linker.
Overrides are applied after pass 3. A later weeklog link of that event joins
it again. Until then, link-run and collect leave it detached, and weeklog
week shows the stored reason override:unlink.

The window flags match weeklog link-run. They choose the stale-branch clock
and the local day.

The database is the same file weeklog collect uses. --db selects another file.`,
		Example: `  weeklog unlink git/0123456789abcdef0123456789abcdef01234567
  weeklog unlink event:4
  weeklog unlink --week 2026-W41 linear/GEN-301`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         runUnlink,
	}
	addLinkFlags(cmd)
	return cmd
}

func runLink(cmd *cobra.Command, args []string) error {
	left, err := parseEventArg(args[0])
	if err != nil {
		return err
	}
	right, err := parseEventArg(args[1])
	if err != nil {
		return err
	}
	if sameEventArg(left, right) {
		return fmt.Errorf("cannot link an event to itself")
	}
	session, err := openLinkSession(cmd)
	if err != nil {
		return err
	}
	defer session.close()

	a, err := resolveEvent(session.ctx, session.st, left)
	if err != nil {
		return err
	}
	b, err := resolveEvent(session.ctx, session.st, right)
	if err != nil {
		return err
	}
	if a.ID == b.ID {
		return fmt.Errorf("cannot link an event to itself")
	}
	if _, err := session.st.AddOverride(session.ctx, store.OverrideLink, a.ID, b.ID); err != nil {
		return err
	}
	return finishOverride(cmd, session, "link", a, b)
}

func runUnlink(cmd *cobra.Command, args []string) error {
	arg, err := parseEventArg(args[0])
	if err != nil {
		return err
	}
	session, err := openLinkSession(cmd)
	if err != nil {
		return err
	}
	defer session.close()

	ev, err := resolveEvent(session.ctx, session.st, arg)
	if err != nil {
		return err
	}
	if _, err := session.st.AddOverride(session.ctx, store.OverrideUnlink, ev.ID, 0); err != nil {
		return err
	}
	return finishOverride(cmd, session, "unlink", ev)
}

func finishOverride(cmd *cobra.Command, session *linkSession, kind string, events ...store.Record) error {
	n, err := session.relink()
	if err != nil {
		return fmt.Errorf("override saved, link failed: %w", err)
	}
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = ev.Source + "/" + ev.SourceID
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "db: %s\noverride: %s %s\n%s\nlink: ok (%s)\n",
		session.dbPath, kind, strings.Join(names, " "), formatLinkWindow(session.window), workItemCount(n))
	return err
}
