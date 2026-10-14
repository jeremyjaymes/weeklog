package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jeremyjaymes/weeklog/internal/store"
)

// eventArg names one stored event.
// byID selects the row id. Otherwise source and sourceID are the dedupe key.
type eventArg struct {
	byID     bool
	id       int64
	source   string
	sourceID string
}

func (a eventArg) String() string {
	if a.byID {
		return fmt.Sprintf("event:%d", a.id)
	}
	return a.source + "/" + a.sourceID
}

func parseEventArg(arg string) (eventArg, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return eventArg{}, fmt.Errorf("event is empty")
	}
	if _, ok := strings.CutPrefix(arg, "item:"); ok {
		return eventArg{}, fmt.Errorf("event %q is a work item; pass SOURCE/SOURCE_ID or event:ID", arg)
	}
	if id, ok, err := parsePrefixedID(arg, "event:"); ok || err != nil {
		if err != nil {
			return eventArg{}, err
		}
		return eventArg{byID: true, id: id}, nil
	}
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
		if id <= 0 {
			return eventArg{}, fmt.Errorf("event %q is not a valid id", arg)
		}
		return eventArg{byID: true, id: id}, nil
	}
	source, sourceID, ok := splitSourceRef(arg)
	if !ok {
		return eventArg{}, fmt.Errorf("event %q must be SOURCE/SOURCE_ID or event:ID", arg)
	}
	return eventArg{source: source, sourceID: sourceID}, nil
}

// explainTarget is either a work item id or an event.
type explainTarget struct {
	item   int64
	event  eventArg
	isItem bool
}

func parseExplainArg(arg string) (explainTarget, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return explainTarget{}, fmt.Errorf("argument is empty")
	}
	if id, ok, err := parsePrefixedID(arg, "item:"); ok || err != nil {
		if err != nil {
			return explainTarget{}, err
		}
		return explainTarget{isItem: true, item: id}, nil
	}
	if id, ok, err := parsePrefixedID(arg, "event:"); ok || err != nil {
		if err != nil {
			return explainTarget{}, err
		}
		return explainTarget{event: eventArg{byID: true, id: id}}, nil
	}
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
		if id <= 0 {
			return explainTarget{}, fmt.Errorf("work item %q is not a valid id", arg)
		}
		return explainTarget{isItem: true, item: id}, nil
	}
	ev, err := parseEventArg(arg)
	if err != nil {
		return explainTarget{}, fmt.Errorf("explain %q must be a work item id, SOURCE/SOURCE_ID, or event:ID", arg)
	}
	return explainTarget{event: ev}, nil
}

func parsePrefixedID(arg, prefix string) (int64, bool, error) {
	rest, ok := strings.CutPrefix(arg, prefix)
	if !ok {
		return 0, false, nil
	}
	label := strings.TrimSuffix(prefix, ":")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return 0, true, fmt.Errorf("%s id is empty", label)
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, true, fmt.Errorf("%s id %q is invalid", label, rest)
	}
	return id, true, nil
}

func splitSourceRef(arg string) (string, string, bool) {
	source, sourceID, ok := strings.Cut(arg, "/")
	source = strings.TrimSpace(source)
	sourceID = strings.TrimSpace(sourceID)
	if !ok || source == "" || sourceID == "" || strings.Contains(source, "/") {
		return "", "", false
	}
	return source, sourceID, true
}

func sameEventArg(a, b eventArg) bool {
	if a.byID || b.byID {
		return a.byID && b.byID && a.id == b.id
	}
	return a.source == b.source && a.sourceID == b.sourceID
}

func resolveEvent(ctx context.Context, st *store.Store, arg eventArg) (store.Record, error) {
	if arg.byID {
		rec, err := st.GetEventByID(ctx, arg.id)
		if err != nil {
			return store.Record{}, err
		}
		return rec, nil
	}
	rec, err := st.GetEvent(ctx, arg.source, arg.sourceID)
	if err != nil {
		return store.Record{}, err
	}
	return rec, nil
}
