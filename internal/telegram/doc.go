// Package telegram is a CHANNEL ADAPTER — it owns telebot wiring, Telegram
// entity rendering, document upload, /commands. Composition lives in
// cmd/aura/app.go. Agent orchestration lives in internal/agent +
// internal/channels/telegram. Cron dispatch lives in internal/cron +
// internal/channels/cron. Do not add new responsibilities here.
package telegram
