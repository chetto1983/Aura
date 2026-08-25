package agui

import "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

// Event is the AG-UI event surface re-exported under the agui package so in-process
// consumers (e.g. the Phase-13 Telegram channel) reference agui.Event, not the external
// events.Event — keeping github.com/ag-ui-protocol/... out of every call site (PRD
// Slice 8: "Type aliases per evitare leak del package esterno nei call sites"). Only the
// subscriber surface is aliased; the SDK is not blanket re-exported.
type Event = events.Event

// EventType is the aliased AG-UI event discriminator (agui.Event.Type() returns it),
// re-exported alongside Event so a consumer can switch on event kind without importing
// the SDK.
type EventType = events.EventType
