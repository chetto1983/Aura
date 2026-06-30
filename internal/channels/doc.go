// Package channels is the daemon channels framework (Phase 13 / Slice 9a, UX-02):
// a Channel interface + Registry that the bootServe lifecycle mounts as a
// fail-soft subsystem, with Telegram (internal/channels/telegram) the first real
// channel.
package channels
